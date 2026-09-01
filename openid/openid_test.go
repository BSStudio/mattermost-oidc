// Copyright (c) 2026 Toowoxx IT GmbH
// Licensed under the AGPL-3.0 license. See LICENSE for details.

package openid

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
)

// mockLogger implements mlog.LoggerIFace for testing
type mockLogger struct{}

func (m *mockLogger) IsLevelEnabled(level mlog.Level) bool                       { return true }
func (m *mockLogger) Trace(msg string, fields ...mlog.Field)                     {}
func (m *mockLogger) Debug(msg string, fields ...mlog.Field)                     {}
func (m *mockLogger) Info(msg string, fields ...mlog.Field)                      {}
func (m *mockLogger) Warn(msg string, fields ...mlog.Field)                      {}
func (m *mockLogger) Error(msg string, fields ...mlog.Field)                     {}
func (m *mockLogger) Critical(msg string, fields ...mlog.Field)                  {}
func (m *mockLogger) Fatal(msg string, fields ...mlog.Field)                     {}
func (m *mockLogger) Log(level mlog.Level, msg string, fields ...mlog.Field)     {}
func (m *mockLogger) LogM(levels []mlog.Level, msg string, fields ...mlog.Field) {}
func (m *mockLogger) With(fields ...mlog.Field) *mlog.Logger                     { return nil }
func (m *mockLogger) Flush() error                                               { return nil }
func (m *mockLogger) Sugar(fields ...mlog.Field) mlog.Sugar                      { return mlog.Sugar{} }
func (m *mockLogger) StdLogger(level mlog.Level) *log.Logger                     { return nil }

// Test claims parsing with all fields
func TestParseOIDCClaims_AllFields(t *testing.T) {
	jsonData := `{
		"sub": "user-123",
		"email": "test@example.com",
		"email_verified": true,
		"preferred_username": "testuser",
		"given_name": "Test",
		"family_name": "User",
		"name": "Test User",
		"picture": "https://example.com/photo.jpg"
	}`

	claims, err := ParseOIDCClaims(strings.NewReader(jsonData))
	if err != nil {
		t.Fatalf("Failed to parse claims: %v", err)
	}

	if claims.Sub != "user-123" {
		t.Errorf("Expected sub 'user-123', got '%s'", claims.Sub)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("Expected email 'test@example.com', got '%s'", claims.Email)
	}
	if !claims.EmailVerified {
		t.Error("Expected email_verified to be true")
	}
	if claims.PreferredUsername != "testuser" {
		t.Errorf("Expected preferred_username 'testuser', got '%s'", claims.PreferredUsername)
	}
	if claims.GivenName != "Test" {
		t.Errorf("Expected given_name 'Test', got '%s'", claims.GivenName)
	}
	if claims.FamilyName != "User" {
		t.Errorf("Expected family_name 'User', got '%s'", claims.FamilyName)
	}
}

// Test claims validation
func TestOIDCClaims_Validate(t *testing.T) {
	tests := []struct {
		name    string
		claims  OIDCClaims
		wantErr bool
	}{
		{
			name: "valid claims",
			claims: OIDCClaims{
				Sub:   "user-123",
				Email: "test@example.com",
			},
			wantErr: false,
		},
		{
			name: "missing sub",
			claims: OIDCClaims{
				Email: "test@example.com",
			},
			wantErr: true,
		},
		{
			name: "missing email",
			claims: OIDCClaims{
				Sub: "user-123",
			},
			wantErr: true,
		},
		{
			name:    "empty claims",
			claims:  OIDCClaims{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.claims.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test conversion to Mattermost user
func TestOIDCClaims_ToUser(t *testing.T) {
	logger := &mockLogger{}

	usePreferred := &model.SSOSettings{UsePreferredUsername: new(true)}
	noPreferred := &model.SSOSettings{UsePreferredUsername: new(false)}

	tests := []struct {
		name              string
		claims            OIDCClaims
		settings          *model.SSOSettings
		wantUsername      string
		wantFirstName     string
		wantLastName      string
		wantEmail         string
		wantAuthData      string
		wantEmailVerified bool
	}{
		{
			name: "preferred_username used when UsePreferredUsername=true",
			claims: OIDCClaims{
				Sub:               "user-123",
				Email:             "Test@Example.com",
				EmailVerified:     true,
				PreferredUsername: "testuser",
				GivenName:         "Test",
				FamilyName:        "User",
			},
			settings:          usePreferred,
			wantUsername:      "testuser",
			wantFirstName:     "Test",
			wantLastName:      "User",
			wantEmail:         "test@example.com", // Lowercase
			wantAuthData:      "user-123",
			wantEmailVerified: true,
		},
		{
			name: "preferred_username ignored when UsePreferredUsername=false (default)",
			claims: OIDCClaims{
				Sub:               "user-123",
				Email:             "Test@Example.com",
				EmailVerified:     true,
				PreferredUsername: "testuser",
				GivenName:         "Test",
				FamilyName:        "User",
			},
			settings:          noPreferred,
			wantUsername:      "test", // email local part, not preferred_username
			wantFirstName:     "Test",
			wantLastName:      "User",
			wantEmail:         "test@example.com",
			wantAuthData:      "user-123",
			wantEmailVerified: true,
		},
		{
			name: "nil settings behaves like UsePreferredUsername=false",
			claims: OIDCClaims{
				Sub:               "user-123",
				Email:             "person@example.com",
				PreferredUsername: "ignored",
			},
			settings:          nil,
			wantUsername:      "person", // email local part
			wantEmail:         "person@example.com",
			wantAuthData:      "user-123",
			wantEmailVerified: false,
		},
		{
			name: "preferred_username split on @ when enabled",
			claims: OIDCClaims{
				Sub:               "user-007",
				Email:             "bond@example.com",
				PreferredUsername: "jbond@corp.example.com",
			},
			settings:          usePreferred,
			wantUsername:      "jbond", // local part of the preferred_username
			wantEmail:         "bond@example.com",
			wantAuthData:      "user-007",
			wantEmailVerified: false,
		},
		{
			name: "falls back to email when preferred_username empty and flag on",
			claims: OIDCClaims{
				Sub:               "user-456",
				Email:             "john.doe@example.com",
				PreferredUsername: "",
			},
			settings:          usePreferred,
			wantUsername:      "john.doe",
			wantFirstName:     "",
			wantLastName:      "",
			wantEmail:         "john.doe@example.com",
			wantAuthData:      "user-456",
			wantEmailVerified: false,
		},
		{
			name: "name split fallback",
			claims: OIDCClaims{
				Sub:           "user-789",
				Email:         "test@example.com",
				EmailVerified: true,
				Name:          "John Doe Smith",
			},
			wantUsername:      "test",
			wantFirstName:     "John",
			wantLastName:      "Doe Smith",
			wantEmail:         "test@example.com",
			wantAuthData:      "user-789",
			wantEmailVerified: true,
		},
		{
			name: "single name",
			claims: OIDCClaims{
				Sub:           "user-abc",
				Email:         "test@example.com",
				EmailVerified: true,
				Name:          "Madonna",
			},
			wantUsername:      "test",
			wantFirstName:     "Madonna",
			wantLastName:      "",
			wantEmail:         "test@example.com",
			wantAuthData:      "user-abc",
			wantEmailVerified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := tt.claims.ToUser(logger, tt.settings)

			if user.Username != tt.wantUsername {
				t.Errorf("Username = %s, want %s", user.Username, tt.wantUsername)
			}
			if user.FirstName != tt.wantFirstName {
				t.Errorf("FirstName = %s, want %s", user.FirstName, tt.wantFirstName)
			}
			if user.LastName != tt.wantLastName {
				t.Errorf("LastName = %s, want %s", user.LastName, tt.wantLastName)
			}
			if user.Email != tt.wantEmail {
				t.Errorf("Email = %s, want %s", user.Email, tt.wantEmail)
			}
			if user.AuthData == nil || *user.AuthData != tt.wantAuthData {
				t.Errorf("AuthData = %v, want %s", user.AuthData, tt.wantAuthData)
			}
			if user.AuthService != model.ServiceOpenid {
				t.Errorf("AuthService = %s, want %s", user.AuthService, model.ServiceOpenid)
			}
			if user.EmailVerified != tt.wantEmailVerified {
				t.Errorf("EmailVerified = %v, want %v", user.EmailVerified, tt.wantEmailVerified)
			}
		})
	}
}

// Test discovery endpoint construction
func TestDiscoveryEndpointFromIssuer(t *testing.T) {
	tests := []struct {
		issuer string
		want   string
	}{
		{
			issuer: "https://keycloak.example.com/realms/main",
			want:   "https://keycloak.example.com/realms/main/.well-known/openid-configuration",
		},
		{
			issuer: "https://keycloak.example.com/realms/main/",
			want:   "https://keycloak.example.com/realms/main/.well-known/openid-configuration",
		},
		{
			issuer: "https://auth0.com",
			want:   "https://auth0.com/.well-known/openid-configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.issuer, func(t *testing.T) {
			got := DiscoveryEndpointFromIssuer(tt.issuer)
			if got != tt.want {
				t.Errorf("DiscoveryEndpointFromIssuer(%s) = %s, want %s", tt.issuer, got, tt.want)
			}
		})
	}
}

// linkCase drives the IsSameUser table.
type linkCase struct {
	name      string
	env       string
	dbUser    *model.User
	oauthUser *model.User
	want      bool
}

// migrating builds an ordinary member mid-migration: an existing account on some
// other auth service, and the OIDC login arriving for the same address.
func migrating(authService, roles string) (*model.User, *model.User) {
	gitlabID := "gitlab-id-789"
	sub := "user-123"
	return &model.User{Email: testMember, Roles: roles, AuthService: authService, AuthData: &gitlabID},
		&model.User{Email: testMember, AuthService: model.ServiceOpenid, AuthData: &sub}
}

const (
	testMember      = "user@example.com"
	testSomeoneElse = "someone.else@example.com"
)

// Test IsSameUser
func TestOpenIDProvider_IsSameUser(t *testing.T) {
	provider := &OpenIDProvider{}

	sub1 := "user-123"
	sub2 := "user-456"

	tests := []linkCase{
		// Case 1: Same provider, same AuthData
		{
			name:      "same OIDC user",
			dbUser:    &model.User{AuthService: model.ServiceOpenid, AuthData: &sub1},
			oauthUser: &model.User{AuthService: model.ServiceOpenid, AuthData: &sub1},
			want:      true,
		},
		// Case 2: Same provider, different AuthData — different person
		{
			name:      "different OIDC user",
			dbUser:    &model.User{AuthService: model.ServiceOpenid, AuthData: &sub1},
			oauthUser: &model.User{AuthService: model.ServiceOpenid, AuthData: &sub2},
			want:      false,
		},
		// Case 2: already OIDC, refused even when the address is listed
		{
			name:      "already OIDC different sub",
			env:       testMember,
			dbUser:    &model.User{Email: testMember, AuthService: model.ServiceOpenid, AuthData: &sub2},
			oauthUser: &model.User{Email: testMember, AuthService: model.ServiceOpenid, AuthData: &sub1},
			want:      false,
		},
	}

	tests = append(tests, ordinaryMigrationCases()...)
	tests = append(tests, privilegedMigrationCases()...)
	tests = append(tests, linkEdgeCases()...)

	// Run every case twice: with a nil request context (Mattermost core always
	// passes one, but the interface permits nil) and with a real one, so the
	// logging path is exercised too.
	contexts := map[string]request.CTX{
		"nil ctx": nil,
		"ctx":     request.EmptyContext(&mockLogger{}),
	}

	for _, tt := range tests {
		for ctxName, rctx := range contexts {
			t.Run(tt.name+"/"+ctxName, func(t *testing.T) {
				t.Setenv(LinkPrivilegedAccountsEnvVar, tt.env)

				got := provider.IsSameUser(rctx, tt.dbUser, tt.oauthUser)
				if got != tt.want {
					t.Errorf("IsSameUser() = %v, want %v", got, tt.want)
				}
			})
		}
	}
}

// ordinaryMigrationCases covers the path the whole user base takes: an
// unprivileged account on any prior auth service, migrating with no list set.
func ordinaryMigrationCases() []linkCase {
	var cases []linkCase

	for _, authService := range []string{"gitlab", "google", "email", "office365", "saml", "ldap", ""} {
		name := authService
		if name == "" {
			name = "password (empty AuthService)"
		}
		dbUser, oauthUser := migrating(authService, model.SystemUserRoleId)
		cases = append(cases, linkCase{
			name:      name + " to OIDC migration",
			dbUser:    dbUser,
			oauthUser: oauthUser,
			want:      true,
		})
	}

	// Roles that carry no authority beyond the account itself.
	for _, roles := range []string{
		"",
		model.SystemUserRoleId,
		model.SystemGuestRoleId,
		model.SystemUserRoleId + " " + model.SystemUserAccessTokenRoleId,
		model.SystemUserRoleId + " " + model.SystemPostAllRoleId,
		model.SystemUserRoleId + " " + model.SystemPostAllPublicRoleId,
	} {
		dbUser, oauthUser := migrating("gitlab", roles)
		cases = append(cases, linkCase{
			name:      "benign roles [" + roles + "] migrate freely",
			dbUser:    dbUser,
			oauthUser: oauthUser,
			want:      true,
		})
	}

	return cases
}

// privilegedMigrationCases covers the accounts a takeover would escalate on:
// refused by default, linkable only while explicitly named. The unknown role is
// the point of the permit-list — a role from a future Mattermost release must
// not become linkable the day it ships.
func privilegedMigrationCases() []linkCase {
	var cases []linkCase

	for _, roles := range []string{
		model.SystemUserRoleId + " " + model.SystemAdminRoleId,
		model.SystemUserRoleId + " " + model.SystemManagerRoleId,
		model.SystemUserRoleId + " " + model.SystemUserManagerRoleId,
		model.SystemUserRoleId + " " + model.SystemReadOnlyAdminRoleId,
		model.SystemUserRoleId + " " + model.SystemCustomGroupAdminRoleId,
		model.SystemUserRoleId + " " + model.SharedChannelManagerRoleId,
		model.SystemUserRoleId + " system_role_from_a_future_release",
	} {
		unlistedDB, unlistedOAuth := migrating("gitlab", roles)
		namedDB, namedOAuth := migrating("gitlab", roles)
		otherDB, otherOAuth := migrating("gitlab", roles)

		cases = append(cases,
			linkCase{
				name:      "privileged [" + roles + "] refused when not named",
				dbUser:    unlistedDB,
				oauthUser: unlistedOAuth,
				want:      false,
			},
			linkCase{
				name:      "privileged [" + roles + "] links when named",
				env:       testSomeoneElse + "," + testMember,
				dbUser:    namedDB,
				oauthUser: namedOAuth,
				want:      true,
			},
			linkCase{
				name:      "privileged [" + roles + "] refused when somebody else is named",
				env:       testSomeoneElse,
				dbUser:    otherDB,
				oauthUser: otherOAuth,
				want:      false,
			},
		)
	}

	return cases
}

// linkEdgeCases covers bots, mismatched or empty emails, and list normalization.
func linkEdgeCases() []linkCase {
	admin := model.SystemUserRoleId + " " + model.SystemAdminRoleId

	botDB, botOAuth := migrating("gitlab", model.SystemUserRoleId)
	botDB.IsBot = true

	mismatchDB, mismatchOAuth := migrating("gitlab", model.SystemUserRoleId)
	mismatchOAuth.Email = testSomeoneElse

	emptyDB, emptyOAuth := migrating("gitlab", model.SystemUserRoleId)
	emptyDB.Email, emptyOAuth.Email = "", ""

	caseDB, caseOAuth := migrating("gitlab", admin)
	caseDB.Email = "User@Example.com"

	return []linkCase{
		{
			name:      "named match is case and whitespace insensitive",
			env:       "  USER@Example.COM , " + testSomeoneElse,
			dbUser:    caseDB,
			oauthUser: caseOAuth,
			want:      true,
		},
		// Bots do not sign in through OIDC; a login matching one is not a migration.
		{
			name:      "bot account refused even when named",
			env:       testMember,
			dbUser:    botDB,
			oauthUser: botOAuth,
			want:      false,
		},
		// Defensive: core matched on email, but do not take that on trust.
		{
			name:      "refused when the emails differ",
			env:       testMember + "," + testSomeoneElse,
			dbUser:    mismatchDB,
			oauthUser: mismatchOAuth,
			want:      false,
		},
		{
			name:      "refused when the email is empty",
			env:       ",,",
			dbUser:    emptyDB,
			oauthUser: emptyOAuth,
			want:      false,
		},
	}
}

// Test isPrivileged
func TestIsPrivileged(t *testing.T) {
	tests := []struct {
		roles string
		want  bool
	}{
		{roles: "", want: false},
		{roles: model.SystemUserRoleId, want: false},
		{roles: model.SystemGuestRoleId, want: false},
		{roles: model.SystemUserRoleId + " " + model.SystemPostAllRoleId, want: false},
		{roles: model.SystemUserRoleId + " " + model.SystemPostAllPublicRoleId, want: false},
		{roles: model.SystemUserRoleId + " " + model.SystemUserAccessTokenRoleId, want: false},
		{roles: "  " + model.SystemUserRoleId + "   ", want: false},
		{roles: model.SystemAdminRoleId, want: true},
		{roles: model.SystemUserRoleId + " " + model.SystemAdminRoleId, want: true},
		{roles: model.SystemUserRoleId + " " + model.SystemManagerRoleId, want: true},
		{roles: model.SystemUserRoleId + " " + model.SystemUserManagerRoleId, want: true},
		{roles: model.SystemUserRoleId + " " + model.SystemReadOnlyAdminRoleId, want: true},
		{roles: model.SystemUserRoleId + " " + model.SystemCustomGroupAdminRoleId, want: true},
		{roles: model.SystemUserRoleId + " " + model.SharedChannelManagerRoleId, want: true},
		// Fail closed: a role this build has never heard of counts as privileged.
		{roles: model.SystemUserRoleId + " system_role_from_a_future_release", want: true},
	}

	for _, tt := range tests {
		t.Run("["+tt.roles+"]", func(t *testing.T) {
			if got := isPrivileged(&model.User{Roles: tt.roles}); got != tt.want {
				t.Errorf("isPrivileged(%q) = %v, want %v", tt.roles, got, tt.want)
			}
		})
	}
}

// Test IsLinkAllowed
func TestIsLinkAllowed(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		email string
		want  bool
	}{
		{name: "unset list", env: "", email: "user@example.com", want: false},
		{name: "whitespace-only list", env: "  ", email: "user@example.com", want: false},
		{name: "single entry", env: "user@example.com", email: "user@example.com", want: true},
		{name: "multiple entries", env: "a@example.com,user@example.com,b@example.com", email: "user@example.com", want: true},
		{name: "normalized entry", env: " USER@EXAMPLE.COM ", email: "User@Example.com", want: true},
		{name: "not listed", env: "a@example.com,b@example.com", email: "user@example.com", want: false},
		{name: "empty email", env: "user@example.com", email: "", want: false},
		{name: "empty email against empty entries", env: ",,", email: "", want: false},
		{name: "substring is not a match", env: "someuser@example.com", email: "user@example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(LinkPrivilegedAccountsEnvVar, tt.env)

			if got := IsLinkAllowed(tt.email); got != tt.want {
				t.Errorf("IsLinkAllowed(%q) with %s=%q = %v, want %v",
					tt.email, LinkPrivilegedAccountsEnvVar, tt.env, got, tt.want)
			}
		})
	}
}

// Test GetUserFromJSON
func TestOpenIDProvider_GetUserFromJSON(t *testing.T) {
	provider := &OpenIDProvider{}

	jsonData := `{
		"sub": "user-123",
		"email": "test@example.com",
		"preferred_username": "testuser",
		"given_name": "Test",
		"family_name": "User"
	}`

	// Create a mock context
	user, err := provider.GetUserFromJSON(nil, bytes.NewReader([]byte(jsonData)), nil, nil)
	if err != nil {
		t.Fatalf("GetUserFromJSON failed: %v", err)
	}

	if user.Email != "test@example.com" {
		t.Errorf("Email = %s, want test@example.com", user.Email)
	}
	if *user.AuthData != "user-123" {
		t.Errorf("AuthData = %s, want user-123", *user.AuthData)
	}
}

// Test GetUserFromJSON with invalid JSON
func TestOpenIDProvider_GetUserFromJSON_InvalidJSON(t *testing.T) {
	provider := &OpenIDProvider{}

	_, err := provider.GetUserFromJSON(nil, strings.NewReader("invalid json"), nil, nil)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// Test GetUserFromJSON with missing required claims
func TestOpenIDProvider_GetUserFromJSON_MissingClaims(t *testing.T) {
	provider := &OpenIDProvider{}

	// Missing sub
	jsonData := `{"email": "test@example.com"}`
	_, err := provider.GetUserFromJSON(nil, strings.NewReader(jsonData), nil, nil)
	if err == nil {
		t.Error("Expected error for missing sub claim")
	}

	// Missing email
	jsonData = `{"sub": "user-123"}`
	_, err = provider.GetUserFromJSON(nil, strings.NewReader(jsonData), nil, nil)
	if err == nil {
		t.Error("Expected error for missing email claim")
	}
}

// Test email normalization
func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Test@Example.com", "test@example.com"},
		{"  test@example.com  ", "test@example.com"},
		{"TEST@EXAMPLE.COM", "test@example.com"},
		{"test@example.com", "test@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeEmail(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeEmail(%s) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

// Benchmark claims parsing
func BenchmarkParseOIDCClaims(b *testing.B) {
	jsonData := `{
		"sub": "user-123",
		"email": "test@example.com",
		"email_verified": true,
		"preferred_username": "testuser",
		"given_name": "Test",
		"family_name": "User",
		"name": "Test User"
	}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseOIDCClaims(strings.NewReader(jsonData))
	}
}
