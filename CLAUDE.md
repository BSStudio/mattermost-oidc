# CLAUDE.md

Guidance for working on this repo, with an emphasis on porting the integration
to a new Mattermost version. The goal of this file is to encode the non-obvious
things that have bitten previous ports so they don't have to be rediscovered.

## What this repo is

A generic OIDC SSO provider for Mattermost, delivered as:

- A standalone Go module (`openid/`) implementing `einterfaces.OAuthProvider`.
- A small patch (`patches/mattermost-vX.Y.Z.patch`) applied to an upstream
  Mattermost checkout — there is **no fork**.

The patch makes exactly four logical changes:

1. `server/cmd/mattermost/main.go` — blank-import the `openid` package so its
   `init()` registers the provider.
2. `server/go.mod` — add the `require`/`replace` pointing at the local module.
3. `server/config/client.go` — expose the OpenID frontend props
   (`EnableSignUpWithOpenId`, button text/color) without a license check.
4. `server/channels/app/user.go` — remove the email-user guard so existing
   accounts link to OIDC on first login (account linking; optional — see README).

## Porting to a new Mattermost version

The `openid/` module code is almost always **interface-stable** — the
`OAuthProvider` interface did not change v10.11 → v11.0, so a port is usually
"regenerate the patch + bump the go.work," not "audit the module." Check
`server/einterfaces/oauthproviders.go` in the target version; only touch
`openid/` if that interface changed.

### Step 1 — regenerate the patch (it WILL fail to apply cleanly)

The four hunks are logically stable, but line context drifts every release.
Past examples: `c request.CTX` was renamed to `rctx request.CTX`; upstream
removed the `ledongthuc/pdf` replace block, shifting the `go.mod` hunk.

Fast path against a sibling upstream checkout (`../mattermost` at the target tag):

```bash
# Does the old patch still apply? -3 shows which hunks conflict.
cd ../mattermost && git apply -3 --check ../mattermost-oidc/patches/mattermost-v<OLD>.patch
```

If it conflicts, re-apply the four changes by hand against the new tag, then:

```bash
git diff server/channels/app/user.go server/cmd/mattermost/main.go \
         server/config/client.go server/go.mod \
  > ../mattermost-oidc/patches/mattermost-v<NEW>.patch
# Verify it applies from a clean tree:
git reset --hard <NEW-tag> && git apply --check ../mattermost-oidc/patches/mattermost-v<NEW>.patch
```

Keep the previous version's patch around — older ESR lines stay supported.

### Step 2 — go.work MUST include `server/public` (non-obvious, recurring)

The build fails without it. The Mattermost server references in-tree
`server/public` symbols that are newer than the tagged `server/public` release
on the Go module proxy (the `v0.1.x` tag lags HEAD). Symptoms are
`undefined: model.AccessControlPolicyVersionV0_2`, `model.SystemServerId`,
`utils.Pager`, `license.Limits`, etc. — all real symbols, just not in the tag.

Mattermost's own `Makefile setup-go-work` target does the same thing
(`go work use ./public`). The go.work must be:

```
use (
    ./mattermost/server
    ./mattermost/server/public
    ./mattermost-oidc
)
```

### Step 3 — build

`server/v8` is not on the module proxy, so set `GOPRIVATE=github.com/mattermost/*`.

```bash
cd ../mattermost/server && GOPRIVATE='github.com/mattermost/*' make build
```

## Gotchas worth not relearning

- **`openid/go.mod` pins a stale pseudo-version** (e.g. an old v10.11 commit).
  Don't bump it and don't panic about the mismatch — the real build resolves
  `server/v8` and `server/public` via go.work/`replace`, which override the pin
  entirely. It only matters for `go test ./...` inside this repo standalone.
- **Downstream builds pin this repo by commit, not a branch or tarball.** After
  changing the patch here, push first; a consumer that clones at a pinned commit
  only picks up the change once that pin is bumped.
- **`GetUserFromIdToken` intentionally returns `(nil, nil)`** to force the
  authenticated UserInfo path — core passes the raw ID token unvalidated. Don't
  "implement" it without JWKS/iss/aud verification.

## Conventions

- The current target is whatever `patches/mattermost-v*.patch` is newest;
  README's Compatibility section should match.
- Use **Entra ID** (not "Azure AD") and **Microsoft 365** (not "Office 365") in
  prose — current Microsoft branding.
- Only claim compatibility with versions actually built and tested; note
  untested IdPs/paths as such rather than implying coverage.
