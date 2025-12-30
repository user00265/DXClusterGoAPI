# DXClusterGoAPI

[![Release Build Status](https://github.com/user00265/dxclustergoapi/actions/workflows/build-release.yml/badge.svg)](https://github.com/user00265/dxclustergoapi/actions/workflows/build-release.yml)
[![Develop Build Status](https://github.com/user00265/dxclustergoapi/actions/workflows/build-develop.yml/badge.svg)](https://github.com/user00265/dxclustergoapi/actions/workflows/build-develop.yml)
[![Docker Image](https://img.shields.io/docker/pulls/user00265/dxclustergoapi?label=Docker%20Pulls)](https://hub.docker.com/r/user00265/dxclustergoapi)
[![Go Report Card](https://goreportcard.com/badge/github.com/user00265/dxclustergoapi)](https://goreportcard.com/report/github.com/user00265/dxclustergoapi)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Simple JSON REST API for DX Cluster spots. Connects to multiple DX clusters and/or POTA API, caches spots in memory, and enhances them with DXCC and LoTW data. Written in Go for performance.

Inspired by [@int2001's DXClusterAPI](https://github.com/int2001/DXClusterAPI) with multi-cluster support and optional Redis caching, as well as removing the need to ping Wavelog for DXCC data making this project truly standalone.

## Purpose

Make the last N spots from DX clusters accessible via JSON API without needing a persistent cluster connection in your client apps. Aggregates spots from multiple sources into one unified stream with rich metadata.

## Features

- Connect to multiple DX clusters simultaneously (including SOTA clusters)
- Optional POTA (Parks On The Air) API integration
- Enhances spots with:
  - DXCC data (continent, entity, flag emoji, CQ/ITU zones, lat/lng) from Club Log
  - LoTW user status from ARRL data
  - Amateur radio band calculation
- Caches configurable number of spots in memory
- Optional Redis/Valkey support for POTA deduplication
- SQLite storage for DXCC/LoTW data with automatic weekly updates
- Lightweight distroless Docker images

## Quick Start

**Prerequisites:** Docker, Docker Compose, and your amateur radio callsign.

1. **Clone and configure:**

   ```bash
   git clone https://github.com/user00265/dxclustergoapi.git
   cd dxclustergoapi
   ```

2. **Edit `docker-compose.yml`** and set your callsign:

   ```yaml
   environment:
     CALLSIGN: "YOUR_CALLSIGN" # Required
     CLUSTERS: '[{"host":"dxfun.com","port":"8000"},{"host":"cluster.sota.org.uk","port":"7300","sota":true}]'
   ```

   Each cluster config supports: `host`, `port`, `call` (defaults to `CALLSIGN`), `loginPrompt`, `sota` flag (true/false), and `channelBuffer`.

3. **Start it:**

   ```bash
   docker-compose up -d
   docker-compose logs -f
   ```

API available at `http://x.x.x.x:8192`

## Using the API

All endpoints return JSON. Base URL: `http://x.x.x.x:8192`

- `GET /spots` - All cached spots
- `GET /spot/14250` - Latest spot at frequency (kHz preferred, also accepts MHz decimals)
- `GET /spots/20m` - Spots on 20 meters (accepts "20m", "20", "40m", "40", etc.
- `GET /spots/source/pota` - Spots from POTA only (options: `cluster`, `sota`, `pota`)
- `GET /spots/callsign/N0CALL` - All spots involving N0CALL
- `GET /spotter/N0CALL` - Spots where N0CALL is the spotter
- `GET /spotted/W1AW` - Spots where W1AW is spotted
- `GET /spots/dxcc/291` - Spots from/to DXCC entity (use DXCC ID number or continent: NA, EU, AS, AF, OC, SA)
- `GET /spots/dxcc/EU` - All spots from/to European stations
- `GET /stats` - Cache statistics

### Example Output

```json
[
  {
    "spotter": "W1AW",
    "spotted": "K7RA",
    "frequency": 14250,
    "message": "CQ DX",
    "when": "2025-01-01T10:00:00.000Z",
    "source": "dx.n9jr.com",
    "band": "20m",
    "mode": "phone",
    "submode": "USB",
    "dxcc_spotter": {
      "cont": "NA",
      "entity": "United States",
      "flag": "🇺🇸",
      "dxcc_id": "291",
      "cqz": "5",
      "ituz": "8",
      "lotw_user": false,
      "lat": "39.0",
      "lng": "-98.0"
    },
    "dxcc_spotted": {
      "cont": "NA",
      "entity": "United States",
      "flag": "🇺🇸",
      "dxcc_id": "291",
      "cqz": "5",
      "ituz": "8",
      "lotw_user": 2,
      "lat": "39.0",
      "lng": "-98.0"
    }
  }
]
```

**Note:** 
- `frequency` is in kHz (integer, e.g., 14250)
- `band` is calculated from frequency (e.g., "20m", "40m", "2m")
- `mode` and `submode` are available for POTA spots; empty strings for DX Cluster spots
- `cqz` (CQ Zone) and `ituz` (ITU Zone) are included in DXCC data
- `lotw_user` is the number of days since last LoTW upload (integer) if a LoTW member, `false` (boolean) if not
- `lat` and `lng` are strings with 1 decimal place precision

## Configuration

Main environment variables (see `docker-compose.yml` for all options):

| Variable                | Default      | Description                                                          |
| ----------------------- | ------------ | -------------------------------------------------------------------- |
| `CALLSIGN`              | _(required)_ | Your callsign for cluster login                                      |
| `CLUSTERS`              | `[]`         | JSON array of cluster configs (overrides legacy single-cluster vars) |
| `WEBPORT`               | `8192`       | HTTP API port                                                        |
| `MAXCACHE`              | `100`        | Number of spots to cache                                             |
| `POTA_INTEGRATION`      | `false`      | Enable POTA API polling                                              |
| `POTA_POLLING_INTERVAL` | `120s`       | POTA poll frequency                                                  |
| `REDIS_ENABLED`         | `false`      | Use Redis for POTA deduplication                                     |
| `REDIS_HOST`            | -            | Redis hostname (required if enabled)                                 |
| `DXCC_UPDATE_INTERVAL`  | `168h`       | How often to refresh DXCC data                                       |
| `LOTW_UPDATE_INTERVAL`  | `168h`       | How often to refresh LoTW data                                       |
| `LOG_LEVEL`             | `notice`     | Log verbosity: `crit`, `error`, `warn`, `notice`, `info`, `debug`    |

### Multi-Cluster Setup

Use `CLUSTERS` JSON array to connect to multiple clusters:

```json
[
  { "host": "dx.n9jr.com", "port": "7300" },
  { "host": "hrd.wa9pie.net", "port": "8000" },
  { "host": "cluster.sota.org.uk", "port": "7300", "sota": true }
]
```

Each cluster config supports: `host`, `port`, `call` (defaults to `DXCALL`), `loginPrompt`, `sota` flag, and `channelBuffer`.

## Building from Source

Requires Go 1.22+

```bash
go mod tidy
go build -o dxcluster-go-api ./cmd/dxcluster-client

# With version info:
go build -ldflags "-X 'github.com/user00265/dxclustergoapi/version.BuildVersion=v1.0.0'" \
  -o dxcluster-go-api ./cmd/dxcluster-client
```

Run locally:

```bash
export CALLSIGN="YOURCALL"
./dxcluster-go-api
```

## Docker

Pull from Docker Hub:

```bash
docker pull user00265/dxclustergoapi:latest
docker run -e CALLSIGN=YOURCALL -p 8192:8192 user00265/dxclustergoapi:latest
```

Or build locally:

```bash
docker build -t dxclustergoapi:local .
```

## License

MIT License - see [LICENSE](LICENSE) for details.

---

**73!** KF0ACN
