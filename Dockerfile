# Stage 1: Build the Go application
FROM golang:1.25-alpine AS builder

# Set build arguments for version info
ARG BUILD_VERSION=unknown
ARG GIT_COMMIT=unknown

# Set working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies. This step is cached if go.mod/go.sum don't change.
RUN go mod download

# Copy the source code
COPY . .

# Build a fully static binary (CGO_ENABLED=0)
# -ldflags to embed version info and strip debug symbols (-s -w) for smaller binary
RUN CGO_ENABLED=0 go build -ldflags "-s -w \
  -X 'github.com/user00265/dxclustergoapi/version.BuildVersion=${BUILD_VERSION}' \
  -X 'github.com/user00265/dxclustergoapi/version.GitCommit=${GIT_COMMIT}'" \
  -o /dxcluster-go-api ./

# Create the /data directory with correct ownership for the non-root user.
# Chainguard static uses UID 65532 (nonroot).
RUN mkdir -p /data && chown 65532:65532 /data

# Stage 2: Hardened runtime image
# Chainguard static: zero known CVEs, rebuilt nightly, non-root by default,
# includes CA certificates for HTTPS. No shell, no package manager.
FROM cgr.dev/chainguard/static:latest

# Set working directory for the application
WORKDIR /app

# Copy the built executable from the builder stage
COPY --from=builder /dxcluster-go-api /app/dxcluster-go-api

# Copy the pre-created /data directory with correct ownership
COPY --from=builder /data /data

# Expose the web port
EXPOSE 8192

# Define the HEALTHCHECK using the binary's subcommand.
HEALTHCHECK --start-period=60s --interval=60s --timeout=5s --retries=3 \
  CMD ["/app/dxcluster-go-api", "healthcheck"]

# Set the entry point to run our application
ENTRYPOINT ["/app/dxcluster-go-api"]

# Default command: run the main 'serve' subcommand.
CMD ["serve"]
