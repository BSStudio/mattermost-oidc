// Copyright (c) 2026 Toowoxx IT GmbH
// Licensed under the AGPL-3.0 license. See LICENSE for details.

// Package openid provides a generic OIDC SSO provider for Mattermost.
// It implements the einterfaces.OAuthProvider interface and can be used
// with any OIDC-compliant identity provider (Keycloak, Auth0, GitLab, Entra ID, Google).
//
// To use this provider, add a blank import in your main.go:
//
//	import _ "github.com/toowoxx/mattermost-oidc/openid"
//
// Then configure the OpenIdSettings in your Mattermost config.
package openid

import (
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/mattermost/mattermost/server/v8/einterfaces"
)

// OpenIDProvider implements the einterfaces.OAuthProvider interface
// for generic OIDC authentication.
type OpenIDProvider struct{}

func init() {
	provider := &OpenIDProvider{}
	einterfaces.RegisterOAuthProvider(model.ServiceOpenid, provider)
}

// GetUserFromJSON parses the UserInfo response from the OIDC provider.
// This is called after the OAuth token exchange when fetching user info.
func (p *OpenIDProvider) GetUserFromJSON(rctx request.CTX, data io.Reader, tokenUser *model.User, settings *model.SSOSettings) (*model.User, error) {
	claims, err := ParseOIDCClaims(data)
	if err != nil {
		return nil, err
	}

	if err = claims.Validate(); err != nil {
		return nil, err
	}

	var logger mlog.LoggerIFace
	if rctx != nil {
		logger = rctx.Logger()
	}
	return claims.ToUser(logger, settings), nil
}

// GetSSOSettings returns the OpenID settings from the Mattermost config.
// If DiscoveryEndpoint is configured, the authorization, token, and userinfo
// endpoints are resolved from the OIDC discovery document automatically.
func (p *OpenIDProvider) GetSSOSettings(_ request.CTX, config *model.Config, service string) (*model.SSOSettings, error) {
	sso := config.OpenIdSettings
	if sso.DiscoveryEndpoint != nil && *sso.DiscoveryEndpoint != "" {
		doc, err := GetDiscovery(*sso.DiscoveryEndpoint)
		if err != nil {
			return nil, err
		}
		sso.AuthEndpoint = &doc.AuthorizationEndpoint
		sso.TokenEndpoint = &doc.TokenEndpoint
		sso.UserAPIEndpoint = &doc.UserInfoEndpoint
	}
	return &sso, nil
}

// GetUserFromIdToken is not implemented. We always return (nil, nil) to let
// Mattermost core fall back to the UserInfo endpoint.
//
// Mattermost core passes the raw ID token from the token endpoint without any
// validation (no signature, iss, or aud checks). While the token is received
// over TLS, we cannot verify it properly without access to the IdP's JWKS and
// expected issuer/audience values — which are not available in this method's
// interface. Rather than parse unverified JWTs, we skip this optimization and
// rely on the authenticated UserInfo endpoint instead.
func (p *OpenIDProvider) GetUserFromIdToken(_ request.CTX, _ string) (*model.User, error) {
	return nil, nil
}

// LinkPrivilegedAccountsEnvVar names the environment variable holding the
// comma-separated email addresses of *privileged* accounts that may still be
// linked to OIDC. Ordinary accounts link without being named; privileged ones
// never do unless listed here.
//
// It exists because admins have to migrate to OIDC too. Keep it empty except
// during the deploy that migrates one, and keep that window short: while an
// address is listed, anyone able to set that address at the IdP can take the
// account over, roles included.
const LinkPrivilegedAccountsEnvVar = "MM_OIDC_LINK_PRIVILEGED_ACCOUNTS"

// RequireVerifiedEmailEnvVar names the environment variable that decides whether
// account linking also requires the IdP to have verified the address it asserts
// — the `email_verified` claim.
//
// Default false, because many IdPs (authentik among them) emit `false` for every
// user unless a property mapping says otherwise, and requiring it there refuses
// every migration. Turn it on once your IdP emits a value driven by real mailbox
// confirmation; a mapping hardcoded to `true` passes this check for everybody and
// buys nothing.
//
// Parsed with strconv.ParseBool: "true", "1", "false", "0" and friends. Anything
// else, including empty, reads as the default.
const RequireVerifiedEmailEnvVar = "MM_OIDC_LINK_REQUIRE_VERIFIED_EMAIL"

// requireVerifiedEmail reports whether linking requires `email_verified: true`.
func requireVerifiedEmail() bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(RequireVerifiedEmailEnvVar)))
	if err != nil {
		return false
	}
	return enabled
}

// benignSystemRoles are the system roles that carry no authority beyond the
// account itself. Every other role counts as privileged.
//
// This is a permit-list on purpose: a system role added by a future Mattermost
// release is treated as privileged until somebody decides otherwise, rather
// than becoming quietly linkable the day it ships.
var benignSystemRoles = map[string]struct{}{
	model.SystemUserRoleId:            {},
	model.SystemGuestRoleId:           {},
	model.SystemPostAllRoleId:         {},
	model.SystemPostAllPublicRoleId:   {},
	model.SystemUserAccessTokenRoleId: {},
}

// isPrivileged reports whether the account holds any system role beyond the
// benign set — system_admin, system_manager, system_user_manager and the rest.
//
// Roles live on the user row and are never recomputed from OIDC claims, so a
// linked account keeps whatever it had. That is what turns a mis-linked login
// from identity theft into privilege escalation, and it is the reason these
// accounts are gated separately.
//
// Team and channel administration are not covered: those live in TeamMembers
// and ChannelMembers, which IsSameUser has no store handle to read.
func isPrivileged(u *model.User) bool {
	for _, role := range strings.Fields(u.Roles) {
		if _, ok := benignSystemRoles[role]; !ok {
			return true
		}
	}
	return false
}

// IsSameUser compares two users to determine if they represent the same OIDC user.
//
// This is called by Mattermost's CreateOAuthUser after it finds an existing user
// by email. If IsSameUser returns true, the core calls UpdateAuthData to migrate
// that account onto the incoming sub; if it returns false, the login fails with
// "already attached to another account".
//
// Three cases are handled:
//  1. Same provider: AuthData matches (normal login, same OIDC sub).
//  2. DB user is already on OIDC with a different sub — a different person. Rejected.
//  3. Cross-provider linking: the DB user is on another auth service (gitlab,
//     email/password, google, …). This is how an existing user base migrates
//     onto OIDC, and it trusts the IdP's email claim to identify the account.
//
// Case 3 is gated by linkDecision rather than allowed outright: the email claim
// is only as trustworthy as the IdP's control over it, so linking is refused for
// the accounts where a wrong answer costs the most.
func (p *OpenIDProvider) IsSameUser(rctx request.CTX, dbUser, oAuthUser *model.User) bool {
	// Case 1: Same AuthData = same user (normal case)
	if dbUser.AuthData != nil && oAuthUser.AuthData != nil {
		if *dbUser.AuthData == *oAuthUser.AuthData {
			return true
		}
	}

	// Case 2: Already OIDC with a different sub = different person.
	if dbUser.AuthService == model.ServiceOpenid {
		return false
	}

	// Case 3: Cross-provider linking.
	allowed, reason := linkDecision(dbUser, oAuthUser)
	logLinkDecision(rctx, allowed, reason, dbUser, oAuthUser)

	return allowed
}

// linkDecision decides whether an existing non-OIDC account may be taken over by
// an incoming OIDC login, and says why. The reason is for the log line — every
// outcome here is worth explaining to whoever reads it later.
func linkDecision(dbUser, oAuthUser *model.User) (bool, string) {
	email := NormalizeEmail(dbUser.Email)

	switch {
	// Core reaches IsSameUser only after matching on email, but do not rely on
	// that: check both sides so this decision holds standalone.
	case email == "" || email != NormalizeEmail(oAuthUser.Email):
		return false, "emails do not match"

	// Bots do not sign in through OIDC. A login matching one is not a migration.
	case dbUser.IsBot:
		return false, "target is a bot account"

	// The IdP did not vouch for this address, so it cannot be used to identify an
	// existing account. Deliberately above the privileged cases: a named admin
	// clears the same bar, and an unverified address is worth knowing about
	// before an admin account moves.
	case requireVerifiedEmail() && !oAuthUser.EmailVerified:
		return false, "email claim is not verified by the IdP"

	// The ordinary case: an existing member migrating to OIDC. No list, no
	// deploy, no ceremony — this is the path the whole user base takes.
	case !isPrivileged(dbUser):
		return true, "unprivileged account"

	case IsLinkAllowed(email):
		return true, "privileged account named in " + LinkPrivilegedAccountsEnvVar

	default:
		return false, "privileged account not named in " + LinkPrivilegedAccountsEnvVar
	}
}

// logLinkDecision records every account-linking decision. A link takes over an
// existing account and is the single most security-relevant event this provider
// produces; a refusal is what an attempted takeover looks like.
func logLinkDecision(rctx request.CTX, allowed bool, reason string, dbUser, oAuthUser *model.User) {
	if rctx == nil {
		return
	}

	sub := ""
	if oAuthUser.AuthData != nil {
		sub = *oAuthUser.AuthData
	}

	fields := []mlog.Field{
		mlog.String("email", NormalizeEmail(dbUser.Email)),
		mlog.String("from_auth_service", dbUser.AuthService),
		mlog.String("sub", sub),
		mlog.String("user_id", dbUser.Id),
		mlog.String("roles", dbUser.Roles),
		mlog.Bool("email_verified", oAuthUser.EmailVerified),
		mlog.Bool("require_verified_email", requireVerifiedEmail()),
		mlog.String("reason", reason),
	}

	if allowed {
		rctx.Logger().Info("OIDC account linking: linking existing account to OIDC", fields...)
		return
	}

	rctx.Logger().Warn("OIDC account linking: refused", fields...)
}

// IsLinkAllowed reports whether this email is named in
// LinkPrivilegedAccountsEnvVar. It only decides the privileged case — ordinary
// accounts link without appearing in the list.
func IsLinkAllowed(email string) bool {
	email = NormalizeEmail(email)
	if email == "" {
		return false
	}

	for _, entry := range strings.Split(os.Getenv(LinkPrivilegedAccountsEnvVar), ",") {
		if NormalizeEmail(entry) == email {
			return true
		}
	}
	return false
}

// OIDCClaimsFromJSON parses OIDC claims from a JSON byte slice.
// Useful for testing and direct JSON parsing.
func OIDCClaimsFromJSON(data []byte) (*OIDCClaims, error) {
	var claims OIDCClaims
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

// NormalizeEmail ensures the email is lowercase.
// Used internally for consistent email comparison.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
