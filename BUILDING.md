BUILDING DXClusterGoAPI
======================

This document explains how to build DXClusterGoAPI from source for development and CI.

Prerequisites
- Go 1.22+ installed and on PATH
- git
- Docker (optional, for building images)

Quick build (local binary)

PowerShell:

```powershell
# Fetch dependencies and tidy
go mod tidy

# Build the CLI binary (default output)
go build -o dxcluster-go-api ./cmd/dxcluster-client

# Add version info at build time if desired
# go build -ldflags "-X 'github.com/user00265/dxclustergoapi/version.BuildVersion=v1.0.0' -X 'github.com/user00265/dxclustergoapi/version.GitCommit=$(git rev-parse --short HEAD)'" -o dxcluster-go-api ./cmd/dxcluster-client
```

Linux / macOS (bash):

```bash
go mod tidy
go build -o dxcluster-go-api ./cmd/dxcluster-client
```

Building the Docker image

This project includes a Dockerfile and a distroless image target for production. Build locally with:

```bash
docker build -t user00265/dxclustergoapi:latest .
```

Include version info as build args:

```bash
docker build --build-arg BUILD_VERSION=1.0.0 \
  --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) \
  -t user00265/dxclustergoapi:1.0.0 .
```

Common build pitfalls
- Ensure `go.mod` is up-to-date (run `go mod tidy`).
- If building inside a container or CI, make sure Go toolchain version >= 1.22.

CI notes
- The GitHub Actions workflow provided in `.github/workflows/ci.yml` runs `go test ./...` and builds Docker images on tagged releases.

If you want, I can add a small Makefile or `mage` tasks to standardize these steps.
