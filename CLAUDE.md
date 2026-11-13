# CLAUDE.md

A generic OIDC SSO provider for Mattermost, delivered as:

- A standalone Go module (`openid/`) implementing `einterfaces.OAuthProvider`.
- A small patch (`patches/mattermost-vX.Y.Z.patch`) applied to an upstream Mattermost checkout — there is **no fork**.

[README.md](README.md) covers setup, configuration and account linking, [docs/deployment-guide.md](docs/deployment-guide.md) the local and Docker builds, and [docs/porting.md](docs/porting.md) porting to a new Mattermost version: follow it for every port.

The patch makes exactly four logical changes:

1. `server/cmd/mattermost/main.go` — blank-import the `openid` package so its `init()` registers the provider.
2. `server/go.mod` — add the `require`/`replace` pointing at the local module.
3. `server/config/client.go` — expose the OpenID frontend props (`EnableSignUpWithOpenId`, button text/color) without a license check.
4. `server/channels/app/user.go` — remove the email-user guard so existing password accounts can link to OIDC on first login (account linking; optional — see README).

## Before calling something done

```bash
GOWORK=off GOPRIVATE='github.com/mattermost/*' go build ./...
GOWORK=off GOPRIVATE='github.com/mattermost/*' go test ./...
docker build -t mattermost-oidc .   # after a patch or Dockerfile change
```

The Go commands build and test the module on its own, against the pins in `go.mod`; only the Docker build compiles the patched Mattermost server. This repository has no CI, so nothing else checks a change.

## Commits and branches

Commit messages and PR titles are plain imperative sentences. PRs are squash-merged: the PR title becomes the commit on `main`, with an empty body.

## Rules

- The current target is whatever `patches/mattermost-v*.patch` is newest; README's Compatibility section should match.
- Use **Entra ID** (not "Azure AD") and **Microsoft 365** (not "Office 365") in prose — current Microsoft branding.
- Only claim compatibility with versions actually built and tested; note untested IdPs/paths as such rather than implying coverage.

### Account linking

Account linking itself is gated in `openid/openid.go` `linkDecision`: ordinary accounts link automatically, accounts holding any system role outside the benign permit-list (`benignSystemRoles`) link only while named in `MM_OIDC_LINK_PRIVILEGED_ACCOUNTS`, and bots never link. The gate is on roles because roles live on the user row and are never recomputed from claims, so a mis-linked admin account keeps `system_admin`. When a new Mattermost release adds a system role, it is privileged by default — decide deliberately before adding it to `benignSystemRoles`. Keep hunk 4 as long as password accounts still need migrating.

`MM_OIDC_LINK_REQUIRE_VERIFIED_EMAIL` adds an `email_verified` requirement on top, off by default because authentik and Entra ID do not emit a meaningful value as shipped. It reads `oAuthUser.EmailVerified`, which holds the raw claim only because core sets `user.EmailVerified = true` *after* the `userByEmail` block that calls `IsSameUser` — re-check that ordering in `server/channels/app/user.go` on every version port, since a move above the block would make the flag always true and the check a no-op.

### Gotchas worth not relearning

- **`go.mod`'s module pins only matter standalone.** The Docker/workspace build resolves `server/v8` and `server/public` from the `go.work`/`replace`, overriding the pins entirely — so a pin that lags HEAD won't break that build, and you needn't chase it mid-development. It _does_ matter for standalone `go test ./...` / `go build ./...` and downstream consumers, so repoint it and `go mod tidy` as part of every version port ([docs/porting.md](docs/porting.md), Step 3).
- **Downstream builds pin this repo by commit, not a branch or tarball.** A consumer that clones at a pinned commit only picks up a change here once that pin is bumped.
- **go.mod pseudo-version timestamps are UTC, not local.** The pin format is `v8.0.0-<YYYYMMDDhhmmss>-<commit12>` where the timestamp is the commit's **UTC** committer date. Deriving it with a local-TZ `git show --date=format:…` produces a value Go rejects at build time (`does not match version-control timestamp (expected …)`). Use `TZ=UTC git show -s --format=%cd --date=format-local:%Y%m%d%H%M%S <commit>`. Both pins (server/v8 require and the server/public replace) must use the same UTC timestamp + commit.
- **Upstream no longer commits `config/config.json`** (gone by v11.8). It is generated from `model.Config` defaults at release time via `go run ./scripts/config_generator` (`OUTPUT_CONFIG=…/config.json`). The Dockerfile generates it in the build stage; don't `COPY` a non-existent `config/config.json`.
- **`GetUserFromIdToken` intentionally returns `(nil, nil)`** to force the authenticated UserInfo path — core passes the raw ID token unvalidated. Don't "implement" it without JWKS/iss/aud verification.
