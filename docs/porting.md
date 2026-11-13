# Porting to a new Mattermost version

The `openid/` module code is almost always **interface-stable** — the `OAuthProvider` interface did not change v10.11 → v11.0, so a port is usually "regenerate the patch + bump the go.work," not "audit the module." Check `server/einterfaces/oauthproviders.go` in the target version; only touch `openid/` if that interface changed.

**It did change at v11.6** ([mattermost/mattermost#30852](https://github.com/mattermost/mattermost/pull/30852)). `GetUserFromJSON` gained a fourth argument: `GetUserFromJSON(rctx request.CTX, data io.Reader, tokenUser *model.User, settings *model.SSOSettings)`. Core resolves the provider's `SSOSettings` (via `GetSSOSettings`) and passes them in, and `SSOSettings` gained `UsePreferredUsername *bool` (defaults to `false`). This is **not** a decorative argument — the built-in providers (gitlab, Entra) read `settings.UsePreferredUsername` to decide the username source, and so does `openid/claims.go` `ToUser`: flag on ⇒ `preferred_username` (split on `@`); flag off (default) ⇒ email local part. If you port to a pre-v11.6 line, the signature must drop that argument again and `ToUser` reverts to a single arg; the per-version patches and the module build are not interchangeable across this boundary. Always diff `oauthproviders.go` **and** the gitlab provider between the old and new tag before regenerating — interface drift here silently breaks the build, and behavioral drift (a new settings flag) silently changes account provisioning.

## Step 1 — regenerate the patch (it WILL fail to apply cleanly)

The four hunks are logically stable, but line context drifts every release. Past examples: `c request.CTX` was renamed to `rctx request.CTX`; upstream removed the `ledongthuc/pdf` replace block, shifting the `go.mod` hunk; v11.2 added a `vmihailenco/msgpack` replace **after** the `tablewriter` replace our go.mod hunk anchored on, so the hunk's trailing context was no longer EOF — the fix is to append the OIDC `require`/`replace` at the true end of go.mod.

Fast path against a sibling upstream checkout (`../mattermost` at the target tag):

```bash
# Does the old patch still apply? -3 shows which hunks conflict.
cd ../mattermost && git apply -3 --check ../mattermost-oidc/patches/mattermost-v<OLD>.patch
```

**Clone upstream with LF endings.** Mattermost ships no `.gitattributes`, so on Windows a global `core.autocrlf=true` checks it out CRLF and the LF patch fails on *every* hunk — including files the target tag never touched:

```bash
git clone -c core.autocrlf=false --depth 1 --branch <NEW-tag> \
  https://github.com/mattermost/mattermost.git ../mattermost
```

A hunk failing on a file with a genuinely empty `git diff <OLD-tag> <NEW-tag>` is the tell that this, not real drift, is the problem.

If it conflicts, re-apply the four changes by hand against the new tag, then:

```bash
git diff server/channels/app/user.go server/cmd/mattermost/main.go \
         server/config/client.go server/go.mod \
  > ../mattermost-oidc/patches/mattermost-v<NEW>.patch
# Verify it applies from a clean tree:
git reset --hard <NEW-tag> && git apply --check ../mattermost-oidc/patches/mattermost-v<NEW>.patch
```

Keep the previous version's patch around — older ESR lines stay supported.

## Step 2 — go.work MUST include `server/public` (non-obvious, recurring)

Use the `go.work` from the README's [Quick Start](../README.md#3-apply-the-patch-to-upstream-mattermost); its note explains why `server/public` must be in it and what the build errors look like without it.

## Step 3 — bump go.mod to mirror upstream, then tidy

On every version update, repoint the module and refresh its deps — don't leave them lagging. Mirror upstream exactly; do **not** pick a newer or older Go than the target tag ships.

1. **Go version: match upstream.** Set the `go` directive in this repo's `go.mod`, in the `go.work`, and the Dockerfile base image to the exact `go` directive in the target tag's `server/go.mod` (and `server/public/go.mod` — they agree). Don't run ahead of upstream's Go.
2. **Repoint both pins** to the target commit — `server/v8` (require) and `server/public` (replace), same commit and same **UTC** pseudo-version timestamp (see the timestamp gotcha in [CLAUDE.md](../CLAUDE.md)). Don't hand-write the version — let Go derive it, because the `server/public` pseudo-version's *base* moves as upstream cuts `server/public/vX.Y.Z` tags (it was `v0.4.1-0.…` at v11.8.1 and `v0.4.3-0.…` at v11.9.0):

   ```bash
   go mod edit -dropreplace github.com/mattermost/mattermost/server/public
   GOFLAGS=-mod=mod GOWORK=off GOPRIVATE='github.com/mattermost/*' \
     go get github.com/mattermost/mattermost/server/v8@<commit> \
            github.com/mattermost/mattermost/server/public@<commit>
   ```

   Then re-add the `replace` with the pseudo-version `go get` resolved. The `replace` is load-bearing, not decorative: a released `server/public` tag sorts *above* a pseudo-version derived from it, so without it MVS can pull a tagged `public` that doesn't match the pinned `server/v8` commit.

   **When that `go get` fails** with `server/v8@<commit> requires server/public@vX.Y.Z, not server/public@<commit>`, upstream's `server/go.mod` requires a `public` tag cut off-branch that sorts above the commit's pseudo-version (first hit at v11.11.1: requires `v0.4.4`, commit resolves to `v0.4.4-0.…`). Set the `replace` to the commit's pseudo-version *first*, then `go get` only `server/v8@<commit>`. The `public` require then reads the tag (`v0.4.4`) and the `replace` pins the commit — expected, not a mistake.
3. **Tidy inside the go.work:** `go mod tidy` to refresh `go.sum` and realign the indirect block to the target's module graph.
4. **Validate standalone** — this catches `go.sum` gaps the workspace build hides: `GOWORK=off GOPRIVATE='github.com/mattermost/*' go build ./... && go test ./...`.

## Step 4 — build

Build with `GOPRIVATE` set (see the README's [Quick Start](../README.md#3-apply-the-patch-to-upstream-mattermost)):

```bash
cd ../mattermost/server && GOPRIVATE='github.com/mattermost/*' make build
```

Then build the Docker image (`docs/deployment-guide.md`, Option 2). Before that, move the runtime stage (`FROM alpine:`) to the Alpine release the `golang:*-alpine` build image is on: nothing else bumps it, since this repo deliberately has no Renovate, and the binary links musl dynamically, so a runtime older than the build stage can fail at start.

```bash
docker run --rm golang:<go-version>-alpine cat /etc/alpine-release
```
