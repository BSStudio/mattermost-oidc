# OIDC SSO Provider for Mattermost

A generic OpenID Connect (OIDC) SSO provider for Mattermost. Any OIDC-compliant IdP should work; we have only verified it against Entra ID.

## Features

- OIDC Discovery: authorization, token, and UserInfo endpoints resolved from `.well-known/openid-configuration`
- Account linking: existing accounts migrate to OIDC on first login — automatic for ordinary accounts, gated for privileged ones
- Attribute sync on each login (via Mattermost's OAuth flow)
- Delivered as a Go module plus a small patch against upstream Mattermost — no fork

## Compatibility

| Mattermost | Patch                                                              | Go     |
| ---------- | ------------------------------------------------------------------ | ------ |
| v11.9.0    | [`mattermost-v11.9.0.patch`](patches/mattermost-v11.9.0.patch)     | 1.26.4 |
| v11.8.1    | [`mattermost-v11.8.1.patch`](patches/mattermost-v11.8.1.patch)     | 1.26.3 |
| v11.2.4    | [`mattermost-v11.2.4.patch`](patches/mattermost-v11.2.4.patch)     | 1.24.6 |
| v11.1.3    | [`mattermost-v11.1.3.patch`](patches/mattermost-v11.1.3.patch)     | 1.24.6 |
| v11.0.7    | [`mattermost-v11.0.7.patch`](patches/mattermost-v11.0.7.patch)     | 1.24.6 |
| v10.11.10  | [`mattermost-v10.11.10.patch`](patches/mattermost-v10.11.10.patch) | 1.24.6 |

> **Why this exists**: Mattermost Team Edition (the libre/AGPL build) ships SAML, Google, and Microsoft 365 SSO behind an enterprise license — only GitLab SSO is enabled there. Many self-hosted deployments worked around this by pointing Mattermost's GitLab SSO at a GitLab instance that itself federated to the real IdP. In v11.0, GitLab SSO has also been moved out of Team Edition, so even that workaround is gone. This module restores OIDC directly in Team Edition, letting Mattermost talk to any OIDC IdP without a license or a GitLab intermediary.

## Quick Start

### 1. Clone this repository

```bash
git clone https://github.com/toowoxx/mattermost-oidc.git
```

### 2. Development with Nix (recommended)

```bash
cd mattermost-oidc
nix develop  # Sets up Go 1.26 and GOPRIVATE automatically
go test ./...
go build ./...
```

### 3. Apply the patch to upstream Mattermost

There is no Mattermost fork — the integration is a `git apply` against an upstream checkout. Clone it as a sibling of this repository:

```bash
git clone --depth 1 --branch v11.9.0 https://github.com/mattermost/mattermost.git ../mattermost
```

Apply the OIDC patch. It adds the `go.mod` `require`/`replace`, the `main.go` blank import, removes the email-user guard in `user.go`, and opens the OpenID frontend props without a license check:

```bash
cd ../mattermost && git apply ../mattermost-oidc/patches/mattermost-v11.9.0.patch
```

(Optional) For an AGPL-only build, remove the enterprise directory and strip its import:

```bash
rm -rf server/enterprise
sed -i '/Enterprise Imports/d; /github.com\/mattermost\/mattermost\/server\/v8\/enterprise/d' \
  server/cmd/mattermost/main.go
```

Create a `go.work` in the common parent so the server resolves `mattermost-oidc` locally:

```bash
cd ..
cat > go.work <<'EOF'
go 1.26.4

use (
    ./mattermost/server
    ./mattermost/server/public
    ./mattermost-oidc
)
EOF
```

**Note:** `server/public` must be in the `use` list — the Mattermost server references in-tree `server/public` symbols that are newer than the tagged release on the module proxy, so omitting it breaks the build. This mirrors Mattermost's own `make setup-go-work`.

**Note:** Mattermost doesn't publish `server/v8` to the Go module proxy. Set `GOPRIVATE=github.com/mattermost/*` when building.

### 4. Configure Mattermost

In `config.json` or via environment variables:

```json
{
  "OpenIdSettings": {
    "Enable": true,
    "Id": "your-client-id",
    "Secret": "your-client-secret",
    "DiscoveryEndpoint": "https://your-idp.com/.well-known/openid-configuration",
    "Scope": "openid email profile",
    "ButtonText": "Login with SSO",
    "ButtonColor": "#0058CC"
  }
}
```

Or using environment variables:

```bash
MM_OPENIDSETTINGS_ENABLE=true
MM_OPENIDSETTINGS_ID=your-client-id
MM_OPENIDSETTINGS_SECRET=your-client-secret
MM_OPENIDSETTINGS_DISCOVERYENDPOINT=https://your-idp.com/.well-known/openid-configuration
```

### 5. Build and run

```bash
cd mattermost/server
make build
./bin/mattermost server
```

See [docs/deployment-guide.md](docs/deployment-guide.md) for the Docker build.

## Configuration Reference

| Setting                | Type   | Default                  | Description                                                                                                                                                      |
| ---------------------- | ------ | ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Enable`               | bool   | `false`                  | Enable OIDC authentication                                                                                                                                       |
| `Id`                   | string | `""`                     | OAuth client ID                                                                                                                                                  |
| `Secret`               | string | `""`                     | OAuth client secret                                                                                                                                              |
| `DiscoveryEndpoint`    | string | `""`                     | OIDC discovery URL. When set, `AuthEndpoint`/`TokenEndpoint`/`UserAPIEndpoint` are resolved from it.                                                             |
| `AuthEndpoint`         | string | `""`                     | Authorization endpoint (ignored if `DiscoveryEndpoint` is set)                                                                                                   |
| `TokenEndpoint`        | string | `""`                     | Token endpoint (ignored if `DiscoveryEndpoint` is set)                                                                                                           |
| `UserAPIEndpoint`      | string | `""`                     | UserInfo endpoint (ignored if `DiscoveryEndpoint` is set)                                                                                                        |
| `Scope`                | string | `"openid email profile"` | OAuth scopes to request                                                                                                                                          |
| `ButtonText`           | string | `"OpenID Connect"`       | Login button text                                                                                                                                                |
| `ButtonColor`          | string | `"#145DBF"`              | Login button color                                                                                                                                               |
| `UsePreferredUsername` | bool   | `false`                  | When `true`, the username is taken from the `preferred_username` claim (local part before `@`). When `false` (default), it is derived from the email local part. |

One setting lives outside `OpenIdSettings`, in the server's process environment:

| Variable                           | Default | Description                                                                                                                                                                                                                                    |
| ---------------------------------- | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MM_OIDC_LINK_PRIVILEGED_ACCOUNTS` | unset   | Comma-separated email addresses of _privileged_ accounts (`system_admin` and friends) that may be linked to OIDC. Ordinary accounts link without being listed; privileged ones never do unless named. See [Account Linking](#account-linking). |

## OIDC Claims Mapping

| OIDC Claim           | Mattermost Field         | Notes                                                                                                                                                             |
| -------------------- | ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sub`                | `AuthData`               | Unique user identifier (required)                                                                                                                                 |
| `email`              | `Email`                  | Required, lowercased                                                                                                                                              |
| `email_verified`     | `EmailVerified`          | Passed through from the IdP                                                                                                                                       |
| `preferred_username` | `Username`               | Used **only** when `UsePreferredUsername` is `true` (split on the first `@`); otherwise the username is the local part of `email`. Sanitized via `CleanUsername`. |
| `given_name`         | `FirstName`              |                                                                                                                                                                   |
| `family_name`        | `LastName`               |                                                                                                                                                                   |
| `name`               | `FirstName` + `LastName` | Used when `given_name`/`family_name` are absent; split on the first space                                                                                         |

## Identity Provider Setup

Entra ID is what we use:

1. Register a new application in Entra ID.
2. Set the redirect URI to `https://your-mattermost.com/signup/openid/complete`.
3. Create a client secret.
4. Discovery endpoint: `https://login.microsoftonline.com/{tenant}/v2.0/.well-known/openid-configuration`.

Other OIDC-compliant IdPs should work the same way — point at their discovery endpoint and supply client ID/secret. We just haven't run them.

## Account Linking

When an OIDC login arrives with a `sub` this Mattermost has never seen, and the email in the token matches an existing **non-OIDC** account (`gitlab`, `google`, `saml`, `ldap`, or a plain password account), that account is moved onto the new `sub` — it keeps its ID, channels, posts and roles, and from then on signs in via OIDC. That is how an existing user base migrates to OIDC, and it happens on the user's own first login: nothing to schedule, nothing to prepare per person.

Linking trusts the IdP's `email` claim to identify the account, so it is refused where a wrong answer would cost the most:

| Existing account                                                                                                                         | Linked on first OIDC login?                            |
| ---------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| Any non-OIDC account holding only `system_user`, `system_guest`, `system_post_all`, `system_post_all_public`, `system_user_access_token` | Yes, automatically                                     |
| Holding any other system role — `system_admin`, `system_manager`, `system_user_manager`, `system_read_only_admin`, …                     | Only while named in `MM_OIDC_LINK_PRIVILEGED_ACCOUNTS` |
| Bot account                                                                                                                              | Never                                                  |
| Already on OIDC with a different `sub`                                                                                                   | Never — a different `sub` is a different person        |

The role check is a permit-list, not a blocklist: a system role introduced by a future Mattermost release counts as privileged until somebody decides otherwise, rather than becoming quietly linkable the day it ships.

### Migrating a privileged account

Admins have to move to OIDC too, so there is an escape hatch:

```bash
MM_OIDC_LINK_PRIVILEGED_ACCOUNTS=admin@example.com,other.admin@example.com
```

Comma-separated, matched case-insensitively, whitespace around entries ignored. Read from the process environment on each login attempt, so changing it needs a restart. Set it, deploy, have them log in, confirm the log line, empty it on the next deploy. While an address sits in that list, anyone who can set that address at the IdP can take the account over — keep the window short.

Every decision is logged with the email, previous auth service, incoming `sub`, roles and the reason: `Info` when an account is linked, `Warn` when one is refused. A refusal is what an attempted takeover looks like, so both are worth alerting on.

### Why the gate is on roles

Mattermost stores roles on the user row and never recomputes them from claims, so a linked account keeps whatever it had. That is what separates a mis-linked ordinary member (identity theft, recoverable) from a mis-linked admin (privilege escalation). Gating on the target's roles blocks the second outcome permanently while leaving the first to the IdP, which is where email trust actually belongs.

Two things it does not cover, deliberately:

- **Team and channel administration** ride along regardless. Those live in `TeamMembers`/`ChannelMembers`, which `IsSameUser` has no store handle to read.
- **`email_verified` is not required.** The module parses the claim, but many IdPs emit `false` for every user by default, so requiring it would refuse every migration. If your IdP emits a truthful value, requiring it in `linkDecision` closes the ordinary-member case too and is a two-line change.

**Verified cases:** GitLab → OIDC and password/email auth → OIDC. Other source auth services (`google`, `office365`, `saml`, `ldap`) are handled symmetrically in code (`openid/openid.go`), but we have not exercised those paths in production.

To remove linking entirely, revert the `server/channels/app/user.go` hunk in the patch: upstream then refuses to link password accounts, and `IsSameUser` still gates the rest. The `main.go`, `client.go`, and `go.mod` hunks are required regardless.

## Security

- State parameter validation is handled by Mattermost's OAuth core (timestamp, nonce, signature; one-time use; 30-minute expiry).
- `sub` is used as `AuthData` — a stable identifier that does not change when the user's email or username changes.
- The `email` claim identifies existing accounts on first login, so it is trusted only where a wrong answer is recoverable: privileged accounts and bots are never linked on the strength of an email alone (see [Account Linking](#account-linking)).
- `GetUserFromIdToken` deliberately returns nothing, so core falls back to the authenticated UserInfo endpoint instead of trusting an unvalidated ID token.
- HTTPS is required for OIDC endpoints in production.

## License

AGPL-3.0 — see [LICENSE](LICENSE).
