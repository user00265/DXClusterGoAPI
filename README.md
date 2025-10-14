# DXClusterGoAPI

[![Release Build Status](https://github.com/user00265/dxclustergoapi/actions/workflows/build-release.yml/badge.svg)](https://github.com/user00265/dxclustergoapi/actions/workflows/build-release.yml)
[![Develop Build Status](https://github.com/user00265/dxclustergoapi/actions/workflows/build-develop.yml/badge.svg)](https://github.com/user00265/dxclustergoapi/actions/workflows/build-develop.yml)
[![Docker Image](https://img.shields.io/docker/pulls/user00265/dxclustergoapi?label=Docker%20Pulls)](https://hub.docker.com/r/user00265/dxclustergoapi)
[![Go Report Card](https://goreportcard.com/badge/github.com/user00265/dxclustergoapi)](https://goreportcard.com/report/github.com/user00265/dxclustergoapi)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A high-performance, configurable Go daemon to aggregate ham radio DX spots from multiple sources (DX clusters, POTA API) and expose them via a JSON REST API. It enriches spots with DXCC and LoTW information, offering powerful filtering and querying capabilities for other amateur radio applications.

Inspired by the [DXClusterAPI Node.js project](https://github.com/int2001/DXClusterAPI), rewritten in Go for efficiency and enhanced features.

## ✨ Features

*   **Multi-Source Spot Aggregation**:
    *   Connects to multiple traditional DX Clusters (via TCP/Telnet, including SOTA-specific clusters) concurrently.
    *   Optionally polls the POTA (Parks On The Air) API for activator spots.
*   **Intelligent Spot Caching**:
    *   Maintains an in-memory cache of the latest `X` spots for rapid API responses (configurable).
    *   Optionally integrates with **Redis/Valkey** for POTA spot deduplication and persistence across restarts, including TLS support for secure connections to Redis/Valkey.
*   **Rich Spot Enrichment**:
    *   **DXCC Lookup**: Enriches spots with comprehensive DXCC (DX Century Club) data (Continent, Entity, DXCC ID, CQ/ITU Zones, Latitude/Longitude, Emoji Flag) for both spotter and spotted callsigns. Data is sourced from Club Log's `cty.xml.gz` file, downloaded periodically and stored in a local **SQLite** database. Features robust callsign parsing logic. This file is periodically updated from Club Log.
    *   **LoTW Status**: Indicates if spotter/spotted callsigns are active LoTW (Logbook of The World) users. Data is periodically downloaded from ARRL's LoTW activity CSV and stored in a local **SQLite** database.
    *   **Band Calculation**: Automatically determines the amateur radio band from the spot frequency.
*   **Powerful JSON REST API (Gin Framework)**:
    *   Provides various endpoints for querying aggregated and enriched spots:
        *   `/spots`: Retrieve all cached spots.
        *   `/spot/:qrg`: Get the freshest spot for a specific frequency (e.g., `/spot/14.250`).
        *   `/spots/:band`: Filter spots by band (e.g., `/spots/20m`).
        *   `/spots/source/:source`: Filter spots by source (`dxcluster`, `pota`, `SOTA`).
        *   `/spots/callsign/:callsign`: Get all spots involving a specific callsign (as spotter or spotted).
        *   `/spotter/:callsign`: Get spots where the callsign is the spotter.
        *   `/spotted/:callsign`: Get spots where the callsign is the spotted station.
        *   `/spots/dxcc/:dxcc`: Filter spots by DXCC entity name (e.g., `/spots/dxcc/United States`).
        *   `/stats`: Get cache statistics.
    *   Includes a `healthcheck` command (not HTTP endpoint) for container health probes.
*   **Highly Configurable**: All settings are controlled via environment variables, making it suitable for Docker and cloud deployments.
*   **Robust & Performant**: Written in Go, designed for concurrency and efficient resource usage, with retry mechanisms (exponential backoff) for external services and graceful shutdowns.
*   **Distroless Docker Images**: Built into minimal `static-debian12` Docker images for enhanced security and reduced size.

## 🚀 Getting Started

These instructions will get your `DXClusterGoAPI` daemon up and running using Docker Compose.

### Prerequisites

*   Docker and Docker Compose installed on your system.
*   A valid amateur radio callsign for connecting to DX clusters.

### Installation & Setup

1.  **Clone the Repository**:
    ```bash
    git clone https://github.com/user00265/dxclustergoapi.git
    cd dxclustergoapi
    ```

2.  **Configure Environment Variables**:
    Open the `docker-compose.yml` file in the project root. You **must** configure at least your callsign, and optionally enable POTA, Redis, and adjust other parameters.

    ```yaml
    services:
      dxclustergoapi:
        environment:
          # ... other configs ...
          DXCALL: "YOUR_CALLSIGN_HERE" # <- REPLACE THIS WITH YOUR CALLSIGN
          CLUSTERS: '[{"host":"dxfun.com","port":"8000","call":"YOUR_CALLSIGN_HERE","password":"","loginPrompt":"login:","cluster":"DXFun"},{"host":"cluster.sota.org.uk","port":"7300","call":"YOUR_CALLSIGN_HERE","loginPrompt":"login:","cluster":"SOTA"}]'
          # ... and other variables as desired ...
    ```
    **Important**: If `CLUSTERS` is populated, it **overrides** `DXHOST`, `DXPORT`, `DXCALL`, `DXPASSWORD`. Ensure your `CLUSTERS` JSON also contains your callsign.

    You can optionally create a `.env` file in the project root to manage environment variables (e.g., `DXCALL=W0CP`, `POTA_INTEGRATION=true`).

3.  **Build and Run with Docker Compose**:
    ```bash
    docker-compose up --build -d
    ```
    This command will:
    *   Build the Go application inside a lightweight Docker image.
    *   Start the `dxclustergoapi` container.
    *   Optionally start a `redis` container (if `REDIS_ENABLED` is `true`).
    *   Begin connecting to DX clusters and/or polling the POTA API.

4.  **Verify Status**:
    Check the container logs:
    ```bash
    docker-compose logs -f dxclustergoapi
    ```
    You should see messages indicating successful connections, data downloads, and spot processing.

## 📖 API Usage

The API will be available at `http://localhost:8192` (or your configured `WEBPORT`). If you set a `WEBURL` (e.g., `/api`), remember to include it in your requests.

**Base URL**: `http://localhost:8192${WEBURL:-/}`

*   **All Spots**: `GET /spots`
*   **Latest Spot by Frequency**: `GET /spot/14.250` (Frequency in MHz)
*   **Spots by Band**: `GET /spots/20m`
*   **Spots by Source**: `GET /spots/source/pota` or `GET /spots/source/DXFun`
*   **Spots by Callsign (any role)**: `GET /spots/callsign/K7RA`
*   **Spots by Spotter Callsign**: `GET /spotter/W1AW`
*   **Spots by Spotted Callsign**: `GET /spotted/K7RA`
*   **Spots by DXCC Entity**: `GET /spots/dxcc/United%20States` (URL-encode spaces)
*   **Cache Statistics**: `GET /stats`

### Example Output (`/spots`):

```json
[
  {
    "spotter": "W1AW",
    "spotted": "K7RA",
    "frequency": 14.25,
    "message": "Test Spot Message",
    "when": "2025-01-01T10:00:00Z",
    "source": "DXFunCluster",
    "band": "20m",
    "spotter_data": {
      "dxcc_info": {
        "cont": "NA",
        "entity": "United States",
        "flag": "🇺🇸",
        "dxcc_id": 291,
        "cqz": 5,
        "ituz": 8,
        "lat": 39,
        "lng": -98
      },
      "lotw_info": null,
      "lotw_user": false
    },
    "spotted_data": {
      "dxcc_info": {
        "cont": "NA",
        "entity": "United States",
        "flag": "🇺🇸",
        "dxcc_id": 291,
        "cqz": 5,
        "ituz": 8,
        "lat": 39,
        "lng": -98
      },
      "lotw_info": {
        "Callsign": "K7RA",
        "LastUploadUTC": "2025-01-01T10:00:00Z"
      },
      "lotw_user": true
    },
    "additional_data": {}
  },
  {
    "spotter": "POTA1",
    "spotted": "K2POTA",
    "frequency": 14.3,
    "message": "SSB POTA @ K-0123 Test Park Someplace",
    "when": "2025-01-01T10:01:00Z",
    "source": "pota",
    "band": "20m",
    "spotter_data": {
      "dxcc_info": {
        "cont": "NA",
        "entity": "United States",
        "flag": "🇺🇸",
        "dxcc_id": 291,
        "cqz": 5,
        "ituz": 8,
        "lat": 39,
        "lng": -98
      },
      "lotw_info": null,
      "lotw_user": false
    },
    "spotted_data": {
      "dxcc_info": {
        "cont": "NA",
        "entity": "United States",
        "flag": "🇺🇸",
        "dxcc_id": 291,
        "cqz": 5,
        "ituz": 8,
        "lat": 39,
        "lng": -98
      },
      "lotw_info": {
        "Callsign": "K2POTA",
        "LastUploadUTC": "2025-01-01T08:00:00Z"
      },
      "lotw_user": true
    },
    "additional_data": {
      "pota_ref": "K-0123",
      "pota_mode": "SSB"
    }
  }
]
```

## ⚙️ Configuration (Environment Variables)

| Variable                  | Default                                | Description                                                               |
| :------------------------ | :------------------------------------- | :------------------------------------------------------------------------ |
| `WEBPORT`                 | `8192`                                 | Port the application listens on for HTTP requests.                        |
| `WEBURL`                  | `/`                                    | Base URL path for API endpoints (e.g., `/api`).                           |
| `MAXCACHE`                | `100`                                  | Maximum number of spots to keep in the in-memory cache.                   |
| `DATA_DIR`                | `/data`                                | Directory to store SQLite database files (DXCC, LoTW) and downloaded XML/CSV. |
| `CLUSTERS`                | `[]` (empty JSON array)                | JSON array of `ClusterConfig` objects. **Overrides** `DXHOST`, `DXPORT`, etc. Example provided in `docker-compose.yml`. |
| `DXHOST`                  | `127.0.0.1`                            | (Legacy) Hostname/IP of a single DX Cluster.                              |
| `DXPORT`                  | `7300`                                 | (Legacy) Port of a single DX Cluster.                                     |
| `DXCALL`                  | `""`                                   | (Legacy) Your callsign for a single DX Cluster login. **REQUIRED if `CLUSTERS` is empty.** |
| `DXPASSWORD`              | `""`                                   | (Legacy) Password for a single DX Cluster login.                          |
| `POTA_INTEGRATION`        | `false`                                | `true` to enable POTA API polling.                                        |
| `POTA_POLLING_INTERVAL`   | `120s`                                 | Interval for polling the POTA API (e.g., `60s`, `5m`). Minimum `30s`.    |
| `DXCC_UPDATE_INTERVAL`    | `168h` (`1 week`)                      | Interval for updating DXCC data from Club Log (e.g., `24h`, `168h`).      |
| `LOTW_UPDATE_INTERVAL`    | `168h` (`1 week`)                      | Interval for updating LoTW user activity data (e.g., `24h`, `168h`).      |
| `REDIS_ENABLED`           | `false`                                | `true` to enable Redis/Valkey for POTA spot deduplication and persistence. |
| `REDIS_HOST`              | `""`                                   | Hostname/IP of the Redis/Valkey server. **Required if `REDIS_ENABLED` is `true`.** |
| `REDIS_PORT`              | `6379`                                 | Port of the Redis/Valkey server.                                          |
| `REDIS_USER`              | `""`                                   | Username for Redis/Valkey (if authentication is required).                |
| `REDIS_PASSWORD`          | `""`                                   | Password for Redis/Valkey.                                                |
| `REDIS_DB`                | `0`                                    | Redis/Valkey database number to use.                                      |
| `REDIS_USE_TLS`           | `false`                                | `true` to enable TLS for Redis/Valkey connection.                         |
| `REDIS_INSECURE_SKIP_VERIFY` | `false`                             | `true` to skip TLS certificate verification (use with caution, e.g., for self-signed in dev). |
| `REDIS_SPOT_EXPIRY`       | `360s` (`6m`)                          | TTL for POTA spots stored in Redis.                                       |
| `LOG_LEVEL`               | (unset) / `warn`                      | Optional runtime log level: `error`, `warn`, `info`, `debug` (or `0`-`3`). Setting this enables more verbose logging for troubleshooting. |
| `DXC_CHANNEL_BUFFER`      | `8`                                    | Default channel buffer size used by DX cluster clients (per-cluster override available via `channelBuffer` in `CLUSTERS` JSON). |
| `DX_API_VERBOSE_SPOT_PIPELINE` | `false`                          | When `true` (or `1`), logs verbose information for the spot aggregation pipeline (useful for debugging). |
| `DX_API_TEST_DEBUG`       | `false`                                | When `true`, registers a test-only `/debug` HTTP endpoint that reports per-source counts (used by integration tests). |
| `DX_API_DUMP_PRE_SHUTDOWN`| `false`                                | When `true`, emit a full goroutine stack dump before shutdown (noisy; for debugging hangs). |
| `DX_API_DUMP_POST_CLEANUP`| `false`                                | When `true`, emit a full goroutine stack dump after deferred cleanup completes (debugging only). |

## 🐳 Docker and Docker Compose

The `DXClusterGoAPI` is designed to run in Docker using a [Distroless `static-debian12` image](https://github.com/GoogleContainerTools/distroless).

### Building the Docker Image

You can build the image locally:

```bash
docker build -t user00265/dxclustergoapi:latest .
# To include version info (recommended):
# docker build --build-arg BUILD_VERSION=1.0.0 --build-arg GIT_COMMIT=$(git rev-parse --short HEAD) -t user00265/dxclustergoapi:1.0.0 .
```

PowerShell example (build and run locally):

```powershell
docker build -t user00265/dxclustergoapi:local .
docker run -e DXCALL=YOURCALL -p 8192:8192 user00265/dxclustergoapi:local
```

### Docker Compose Health Check

The `docker-compose.yml` includes a `healthcheck` that uses a subcommand of the main binary.

```yaml
# docker-compose.yml snippet
services:
  dxclustergoapi:
    # ...
    healthcheck:
      test: ["CMD", "/app/dxcluster-go-api", "healthcheck"]
      interval: 60s
      timeout: 5s
      retries: 3
      start_period: 60s
    # ...
```

This health check command (`/app/dxcluster-go-api healthcheck`) verifies the application's internal state (e.g., database connections, Redis connection if enabled).

## 🛠️ Development and Contributing

### Build from Source

Requires Go 1.22+.

```bash
go mod tidy
go build -o dxcluster-go-api ./cmd/dxcluster-client
# To build with version information (for User-Agent string):
# go build -ldflags "-X 'github.com/user00265/dxclustergoapi/internal/version.BuildVersion=v0.1.0' -X 'github.com/user00265/dxclustergoapi/internal/version.GitCommit=$(git rev-parse --short HEAD)'" -o dxcluster-go-api ./cmd/dxcluster-client
```

### Running Tests

```bash
go test -v ./...
```

For integration tests that might require network access or external services (mocked via `httptest` and `miniredis`):

```bash
go test -v -tags=integration ./cmd/dxcluster-client
```

### GitHub Actions (CI)

The `.github/workflows/ci.yml` workflow automatically builds, tests, and (on tagged releases) builds Docker images for the project.

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---
