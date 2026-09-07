# Centralised Logging System — Plan

## 1. Goals
- Centralised log ingestion for multiple services written in Go and Python.
- Producers push log events to **Kafka**.
- A single **Go consumer service** reads from Kafka and persists events to **Parquet** files on local disk.
- The same Go service exposes an **HTTP REST API** to query logs by `logid` and/or `project`.
- Ship lightweight **Go SDK** and **Python SDK** so application code can publish with minimal effort.
- Entire stack runnable in **Docker Compose** for local dev.

---

## 2. High-Level Architecture

```
 ┌────────────────┐    ┌────────────────┐    ┌─────────────────────────┐    ┌────────────┐
 │  App (Go/Py)   │───▶│  Kafka topic   │───▶│  log-consumer (Go svc)  │───▶│ Parquet    │
 │  via SDK       │    │  `logs`        │    │   - consume             │    │ files      │
 └────────────────┘    └────────────────┘    │   - write parquet       │    │ /data/*.pq │
                                             │   - HTTP query API      │    └────────────┘
                                             └─────────────────────────┘
```

Components:

| Component         | Language | Role |
|------------------|----------|------|
| `backend`  | Go       | Kafka consumer, Parquet writer, HTTP query API |
| `sdk/go`   | Go       | Producer-side Go client library (logging-go-sdk) |
| `sdk/py`   | Python   | Producer-side Python client library (logging-py-sdk) |
| `kafka`          | -        | Single-node Kafka + Zookeeper (or KRaft mode) |
| `data`           | volume   | Mounted directory holding Parquet files |

---

## 3. Kafka Design

- **Topic:** single shared topic `logs`.
- **Partitions:** start with `3`, configurable. Partitioning key = `project` so all events for one project land on the same partition (helps per-project locality, while still allowing parallel consumers).
- **Replication:** `1` for single broker dev, `2`+ recommended.
- **Retention:** `7d` by default (configurable).
- **Message format:** UTF-8 JSON (one log event per Kafka record).
- **Compression:** producer-side `snappy` to reduce broker footprint.

### Wire schema (one log record)

```json
{
  "timestamp": "2026-09-06T10:23:45.123Z",
  "project":   "billing-service",
  "logid":     "req-7f2c-9a31-44e0",
  "level":     "INFO",
  "message":   "charge succeeded amount=99.00"
}
```

Notes:
- `timestamp` is RFC3339 UTC. Producers fill this in if absent; consumer can backfill.
- `logid` is application-generated correlation id (request id, trace id, etc.).
- `level` ∈ `DEBUG | INFO | WARN | ERROR | FATAL`.
- `message` is free-form (string).

---

## 4. Go Consumer Service (`backend/`)

Single binary with two modes of operation co-existing in one process:

1. **Kafka consumer goroutine(s)** — read from `logs`, decode, append to Parquet.
2. **HTTP server** — serves query endpoints.

### Tech choices
- Kafka client: `github.com/segmentio/kafka-go` (simple, no CGo) or `github.com/twmb/franz-go` (faster, modern). Recommendation: `kafka-go` for simplicity.
- Parquet: `github.com/parquet-go/parquet-go` (pure Go, actively maintained).
- HTTP: `net/http` + `chi` router (lightweight).
- Config: env vars via `github.com/caarlos0/env` or `viper`.

### Folder layout

```
backend/
├── main.go
├── internal/
│   ├── consumer/        # Kafka consumer loop
│   ├── parquet/         # Writer + rotation logic
│   ├── api/             # HTTP query handlers
│   ├── model/           # LogEvent struct
│   └── config/          # Env-based config
├── Dockerfile
└── go.mod

sdk/
├── go/                  # Go SDK (logging-go-sdk)
└── py/                  # Python SDK (logging-py-sdk)
```

`frontend/` is reserved for a future UI (logs explorer, query builder). For now it contains only `.gitkeep` and a placeholder README.

### Consumer flow

1. Subscribe to `logs` from earliest offset (consumer group: `log-collector`).
2. Decode JSON → `LogEvent`.
3. Hand off to Parquet writer (in-memory buffered writer).
4. Commit Kafka offset **only after** the event is fsynced to disk (at-least-once delivery).
5. On shutdown: flush buffer, close writer, exit cleanly.

---

## 5. Parquet Storage

- **Path:** `/data/parquet/` inside the container (mounted to host volume).
- **File naming:** `logs-<unix-seconds>-<seq>.parquet` where `<seq>` increments if multiple files opened in the same second.
- **Rotation:** when current writer's buffered size reaches **128 MB** (configurable via `PARQUET_ROTATE_BYTES`), close file, open a new one.
- **At file close:** `fsync`, then write a sidecar index file.

### Schema (Parquet)

| Column     | Type           | Notes |
|------------|----------------|-------|
| `timestamp`| `TIMESTAMP`    | microsecond precision |
| `project`  | `BYTE_ARRAY` (UTF-8) | low cardinality, dictionary-encoded |
| `logid`    | `BYTE_ARRAY` (UTF-8) | string id |
| `level`    | `BYTE_ARRAY` (UTF-8) | dictionary-encoded |
| `message`  | `BYTE_ARRAY` (UTF-8) | free-form |

### Index sidecar (`*.idx.json`)

For fast lookups without scanning every Parquet file:

```json
{
  "file":       "logs-1757150400-0001.parquet",
  "started":    "2026-09-06T10:00:00Z",
  "ended":      "2026-09-06T10:11:23Z",
  "row_count":  84213,
  "projects":   ["billing-service", "auth-service"],
  "size_bytes": 134217728
}
```

This allows the query engine to skip files whose time/project ranges don't intersect the query.

---

## 6. Query HTTP API

Base path: `http://localhost:8080`

### Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/healthz` | Liveness probe |
| GET | `/query` | Query logs |
| GET | `/files` | List Parquet files (debug) |

### `GET /query` parameters

| Param      | Type   | Required | Description |
|------------|--------|----------|-------------|
| `project`  | string | one of   | Filter by project |
| `logid`    | string | one of   | Filter by logid |
| `from`     | RFC3339 | no      | Lower bound timestamp |
| `to`       | RFC3339 | no      | Upper bound timestamp |
| `level`    | string | no       | Minimum level filter |
| `limit`    | int    | no       | Max rows (default 100, max 1000) |
| `order`    | enum   | no       | `asc` / `desc` by timestamp (default desc) |

Either `project` or `logid` is required.

### Response

```json
{
  "count": 42,
  "results": [
    {
      "timestamp": "2026-09-06T10:23:45.123Z",
      "project":   "billing-service",
      "logid":     "req-7f2c-9a31-44e0",
      "level":     "INFO",
      "message":   "charge succeeded"
    }
  ]
}
```

### Query algorithm

1. Read `*.idx.json` files in `/data/parquet/`.
2. Filter index entries by:
   - time range overlap (`from`/`to`),
   - `project` membership (if specified).
3. For each surviving file, use parquet-go's row-group + column projection to read only relevant columns.
4. Apply `logid` and remaining filters in memory (Parquet predicate pushdown for `project`/`level`).
5. Merge, sort, limit, return.

Index is loaded into memory once at startup and refreshed on a timer (e.g. every 30s) or via SIGHUP.

---

## 7. Go SDK (`logging-go-sdk`)

Module: `github.com/<user>/logging-sdk-go`

### Public API

```go
type Client struct { /* ... */ }

type Option func(*Client)

func New(bootstrap string, project string, opts ...Option) (*Client, error)
func (c *Client) Log(ctx context.Context, level, logid, message string) error
func (c *Client) Debug(ctx context.Context, logid, format string, args ...any) error
func (c *Client) Info (ctx context.Context, logid, format string, args ...any) error
func (c *Client) Warn (ctx context.Context, logid, format string, args ...any) error
func (c *Client) Error(ctx context.Context, logid, format string, args ...any) error
func (c *Client) Close() error
```

### Options

- `WithAsync(workers int)` — buffered channel + background workers; `Log` returns immediately. Default: synchronous.
- `WithBatchSize(n)` and `WithFlushInterval(d)` — flush threshold.
- `WithLogger(logrus/zerolog/zap bridge)` — optional integration adapters.
- `WithMinLevel(Level)` — drop below this level client-side.

### Behavior

- Serialise `LogEvent` as JSON.
- Produce via `kafka-go.Writer` with `Balancer = &hash.Hash{}` keyed by `project`.
- Async mode: ring buffer of `4096`, drop-oldest with metric counter if Kafka is slow (configurable: drop or block).
- Graceful `Close()` drains buffer, waits up to 5s, then closes writer.

### Example

```go
log, _ := logging.New("kafka:9092", "billing-service")
defer log.Close()

log.Info(ctx, "req-7f2c", "charge succeeded amount=%.2f", 99.00)
```

---

## 8. Python SDK (`logging-py-sdk`)

Package: `logging_sdk` (import as `import logging_sdk`).

### Public API

```python
class Client:
    def __init__(
        self,
        bootstrap: str,
        project: str,
        *,
        async_send: bool = True,
        batch_size: int = 100,
        flush_interval: float = 1.0,
        min_level: str = "DEBUG",
    ): ...

    def log(self, level: str, logid: str, message: str) -> None: ...
    def debug(self, logid: str, message: str, *args) -> None: ...
    def info (self, logid: str, message: str, *args) -> None: ...
    def warn (self, logid: str, message: str, *args) -> None: ...
    def error(self, logid: str, message: str, *args) -> None: ...
    def close(self) -> None: ...
```

### Behavior

- Built on `confluent-kafka` (high-performance, librdkafka) for sync path, or `aiokafka` for `async_send=True` in async code.
- Auto-fill `timestamp` (UTC ISO 8601) if caller doesn't provide it.
- Background thread flushes batches at `flush_interval` or when `batch_size` reached.
- Optional stdlib `logging.Handler` adapter: `LoggingKafkaHandler` — drop into existing `logging` config.

### Example

```python
from logging_sdk import Client
log = Client("kafka:9092", project="auth-service")
log.info("login-abc-123", "user %s authenticated", "alice")
log.close()
```

---

## 9. Docker Compose Topology

Each top-level project (`backend/`, `frontend/`) ships its **own** `docker-compose.yml`. They are run independently:

- `backend/docker-compose.yml` — Kafka + collector + Parquet volume.
- `frontend/docker-compose.yml` — placeholder for the future UI (just a service definition with `image: nginx:alpine` serving static placeholder content; no real app until the UI is built).

### `backend/docker-compose.yml`

```yaml
services:
  kafka:
    container_name: logging-kafka
    image: bitnami/kafka:3.7
    environment:
      KAFKA_CFG_NODE_ID: "0"
      KAFKA_CFG_PROCESS_ROLES: controller,broker
      KAFKA_CFG_LISTENERS: PLAINTEXT://:9092,CONTROLLER://:9093
      KAFKA_CFG_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
      KAFKA_CFG_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_CFG_CONTROLLER_QUORUM_VOTERS: "0@kafka:9093"
      ALLOW_PLAINTEXT_LISTENER: "yes"
    ports: ["9092:9092"]

  collector:
    container_name: logging-collector
    build: .
    depends_on: [kafka]
    environment:
      KAFKA_BROKERS: kafka:9092
      KAFKA_TOPIC: logs
      PARQUET_DIR: /data/parquet
      PARQUET_ROTATE_BYTES: "134217728"
      HTTP_ADDR: 8080
    ports: ["8080:8080"]
    volumes:
      - ./data:/data
```

Run from `backend/`: `docker compose up -d`.

Volumes:
- `./backend/data` mounted as `/data` in `collector` so Parquet files persist on host.

### `frontend/docker-compose.yml`

```yaml
services:
  ui:
    image: nginx:alpine
    volumes:
      - ./public:/usr/share/nginx/html:ro
    ports:
      - "3000:80"
```

Run from `frontend/`: `docker compose up -d`. Until the UI is built, `./frontend/public/index.html` will just say "frontend not yet implemented".

### Why split?

- Lets the backend run and ship on its own (it's the part that actually works first).
- Frontend dev can iterate independently against the backend's `localhost:8080`.
- No shared network dependency — frontend can be started/stopped without affecting Kafka ingestion.

---

## 10. Implementation Order

1. **Bootstrap repo**: `docker-compose.yml`, `README.md` skeleton.
2. **Kafka topic** auto-created via producer admin (or `kafka-topics` script).
3. **`backend` skeleton**: config + main + empty consumer + HTTP server.
4. **Parquet writer + rotation** with a unit test (synthetic producer feeding the writer directly).
5. **Kafka consumer integration** with the writer, fsync-before-commit.
6. **Index sidecar** generation on file close + index loader for API.
7. **Query API** with the index-aware scan.
8. **Go SDK** with sync + async paths and a small integration test.
9. **Python SDK** with sync + async paths and an integration test.
10. **End-to-end smoke test**: sample app publishes 10k events, verify Parquet row count, verify `/query` returns expected results.
11. **Docs**: quickstart in README (already implicitly covered by PLAN.md).

---

## 11. Open Questions / Future Work

- **Auth on query API**: none planned for v1; assume network-level isolation. Add bearer-token later.
- **Retention/cleanup**: cron-style job to delete Parquet files older than N days.
- **Schema evolution**: `message` field is opaque string — easy to evolve later. Could add `attributes` map.
- **Compression**: Parquet defaults to `snappy` already; can tune via writer properties.
- **Backpressure**: if Kafka is slow, SDKs drop-oldest; should expose metric + DLQ topic later.
- **Multi-host Kafka**: switch from KRaft single-node to a 3-broker cluster when moving beyond dev.