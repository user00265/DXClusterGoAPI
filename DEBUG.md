DEBUGGING DXClusterGoAPI
========================

This document describes runtime debugging and testing techniques, environment variables, and tips to diagnose issues.

Run tests

```powershell
# Run all unit and integration tests
go test -v ./...

# Run a single package (e.g., cluster)
go test -v ./internal/cluster -run TestClient_ConnectAndParse
```

Enable verbose spot pipeline traces

- Useful when you want to see every spot as it flows through the aggregator and enrichment pipeline.

```powershell
$env:DX_API_VERBOSE_SPOT_PIPELINE = '1'
go test -v ./cmd/dxcluster-client -run TestMainIntegration
```

Set runtime log level

- `LOG_LEVEL` can be used to change the logging verbosity at startup. Values: `error`, `warn`, `info`, `debug` (or numeric `0`-`3`).

```powershell
# Example: enable debug logs
$env:LOG_LEVEL = 'debug'
# Run app or tests
go test -v ./...
```

Test-only HTTP debug endpoint

- When `DX_API_TEST_DEBUG=1`, the application registers a `/debug` route that returns a JSON object with per-source spot counts. This is used by integration tests.

```powershell
$env:DX_API_TEST_DEBUG = '1'
# Start the app (or run the integration test harness)
# Then query: GET /api/debug
```

Dump goroutine stacks

- Two environment variables control when full goroutine stack dumps are emitted (they are noisy; use only for debugging hangs):
  - `DX_API_DUMP_PRE_SHUTDOWN=1` — dump stacks immediately before shutdown.
  - `DX_API_DUMP_POST_CLEANUP=1` — dump stacks after deferred cleanup completes.

Example:

```powershell
$env:DX_API_DUMP_PRE_SHUTDOWN = '1'
$env:DX_API_DUMP_POST_CLEANUP = '1'
# Run app/tests and observe stack dumps on shutdown
```

Channel buffer configuration

- The DX cluster client channels default to `8`. You can change the global default via `DXC_CHANNEL_BUFFER` or per-cluster by setting `channelBuffer` in the `CLUSTERS` JSON.

```powershell
# Global default for all cluster clients
$env:DXC_CHANNEL_BUFFER = '16'

# Per-cluster override (example in docker-compose or CLUSTERS JSON)
# [{"host":"dxfun.com","port":"8000","call":"CALL","channelBuffer":32}]
```

Observability and logs

- The repository provides a leveled logging package. Use `LOG_LEVEL` to control which messages appear. By default, the project logs at `warn` level to avoid noisy CI output.
- To debug more detailed behaviour such as parsing failures, set `LOG_LEVEL=info` or `LOG_LEVEL=debug`.

Reproducing flakey connect/read issues

- Tests and local harness use mock TCP servers; you can reproduce connection timeouts and server errors by running the unit tests in `internal/cluster` or by creating a simple TCP server that accepts a connection but then closes or sends malformed data.

If you'd like, I can add a small `debug.sh` (or PowerShell) helper to start the app with common debug env vars set.
