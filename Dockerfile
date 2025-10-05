# Stage 1: Build the Go application
FROM golang:1.22-alpine AS builder

# Set build arguments for version info
ARG BUILD_VERSION=unknown
ARG GIT_COMMIT=unknown

# Set working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Download dependencies. This step is cached if go.mod/go.sum don't change.
# CGO_ENABLED=0 is important for static binaries compatible with distroless/static
RUN CGO_ENABLED=0 go mod download

# Copy the source code
COPY . .

# Build the main application binary
# -ldflags to embed version info into the binary
# Output binary to /dxcluster-go-api for easy copying in the next stage
RUN CGO_ENABLED=0 go build -ldflags "-X 'github.com/user00265/dxclustergoapi/internal/version.BuildVersion=${BUILD_VERSION}' \
                      -X 'github.com/user00265/dxclustergoapi/internal/version.GitCommit=${GIT_COMMIT}'" \
                      -o /dxcluster-go-api ./cmd/dxcluster-client

# Stage 2: Create the final Distroless image
# Use 'static-debian12' for truly static Go binaries.
# This assumes glebarez/sqlite (pure Go) which does not require glibc.
FROM gcr.io/distroless/static-debian12:latest

# Create data directory. This is essential for SQLite DBs.
# The `static-debian12` image is extremely minimal.
# os.MkdirAll in our Go app will handle creating this at runtime.
RUN mkdir -p /data

# Set working directory for the application
WORKDIR /app

# Copy the built executable from the builder stage
COPY --from=builder /dxcluster-go-api /app/dxcluster-go-api

# Expose the web port
EXPOSE 8192

# Define the HEALTHCHECK using the binary's subcommand.
# --start-period: gives the container time to initialize without failing the health check.
# --interval: how often to run the check.
# --timeout: how long to wait for the command to return.
# --retries: how many consecutive failures before marking as unhealthy.
HEALTHCHECK --start-period=60s --interval=60s --timeout=5s --retries=3 \
  CMD ["/app/dxcluster-go-api", "healthcheck"]

# Set the entry point to run our application
# The entrypoint will always be our main binary.
ENTRYPOINT ["/app/dxcluster-go-api"]

# Default command: run the main 'serve' subcommand.
CMD ["serve"]