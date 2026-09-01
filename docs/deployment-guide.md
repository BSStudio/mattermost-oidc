# Deployment Guide

Building the Mattermost server binary or Docker image with the OIDC patch applied. How you run the result — systemd on a VM, Docker, Kubernetes, whatever — is up to you.

## Option 1: Local Build

Build Mattermost locally with the OIDC module. There is no Mattermost fork — the integration is a `git apply` against an upstream checkout.

```bash
# Clone upstream Mattermost at the version the patch targets
git clone --depth 1 --branch v11.9.0 https://github.com/mattermost/mattermost.git

# Clone the OIDC module as a sibling (not inside)
git clone https://github.com/toowoxx/mattermost-oidc.git

# Apply the OIDC patch (go.mod require/replace, main.go import,
# user.go email-migration change, and client.go license-gate bypass)
cd mattermost
git apply ../mattermost-oidc/patches/mattermost-v11.9.0.patch

# (Optional) AGPL-only build: remove enterprise and strip its import
rm -rf server/enterprise
sed -i '/Enterprise Imports/d; /github.com\/mattermost\/mattermost\/server\/v8\/enterprise/d' \
    server/cmd/mattermost/main.go

# Set up a go.work at the common parent so the server resolves
# mattermost-oidc locally (Mattermost doesn't publish server/v8 via the proxy).
# server/public must be included too: the server references in-tree public
# symbols newer than the tagged release, so omitting it breaks the build.
cd ..
cat > go.work <<'EOF'
go 1.26.4

use (
    ./mattermost/server
    ./mattermost/server/public
    ./mattermost-oidc
)
EOF

# Build
cd mattermost/server
GOPRIVATE='github.com/mattermost/*' make build-linux-amd64

# The binary is at ./bin/mattermost
```

## Option 2: Docker Build

The `Dockerfile` at the root of this repository does the same thing inside a container: clones upstream at the version in `MM_VERSION`, applies the patch, strips the enterprise directory, and builds a Team Edition binary on Alpine.

```bash
docker build --build-arg MM_VERSION=11.9.0 -t your-registry/mattermost-oidc:11.9.0 .
docker push your-registry/mattermost-oidc:11.9.0
```

The image exposes `8065` and runs `mattermost server` as a non-root user.

## Runtime configuration

`OpenIdSettings` is read from `config.json` or `MM_OPENIDSETTINGS_*` as usual. One setting is read straight from the server process's environment instead:

```bash
MM_OIDC_LINK_PRIVILEGED_ACCOUNTS=admin@example.com
```

Existing non-OIDC accounts are linked to OIDC on their owner's first OIDC login. That is automatic for ordinary accounts and needs no configuration; accounts holding `system_admin` or another privileged system role are refused unless their address is listed here, and bot accounts are always refused. Deploy with it empty, set it only for the deploy that migrates an admin, and empty it again afterwards — it is read per login attempt but from the process environment, so each change means restarting the server (or rolling the pod).

See the [Account Linking](../README.md#account-linking) section of the README for the full rules and the log lines to watch for.
