# Centralised Logging System

Kafka → Parquet log pipeline with a Go query API and lightweight Go /
Python producer SDKs.

```
 ┌────────────────┐    ┌────────────────┐    ┌─────────────────────────┐    ┌────────────┐
 │  App (Go/Py)   │───▶│  Kafka topic   │───▶│  log-consumer (Go svc)  │───▶│ Parquet    │
 │  via SDK       │    │  `logs`        │    │   - consume             │    │ files      │
 └────────────────┘    └────────────────┘    │   - write parquet       │    │ /data/*.pq │
                                             │   - HTTP query API      │    └────────────┘
                                             └─────────────────────────┘
```

The full design lives in [`PLAN.md`](./PLAN.md).

## Repository layout

```
backend/        Go service: Kafka consumer + Parquet writer + HTTP query API
frontend/       Reserved for the future UI (placeholder only)
sdk/
  go/           Go producer SDK
  python/       Python producer SDK
PLAN.md         Architecture, wire schema, query API, implementation order
```

## Quickstart

### 1. Run the backend stack

```bash
cd backend
docker compose up -d
docker compose ps        # wait until collector is healthy
docker compose logs -f collector
```

The compose stack brings up:

- `logging-kafka` — KRaft single-node Kafka broker on `:9092`.
- `logging-collector` — the Go consumer + HTTP API on `:8080`.

Parquet files are written to the host directory mounted into the
container (see `backend/docker-compose.yml`).

### 2. Publish a few events

**Go**

```go
import loggingsdk "github.com/marcuspeh/logging-sdk-go"

log, _ := loggingsdk.New("localhost:9092", "billing-service")
defer log.Close()

ctx := loggingsdk.WithLogID(context.Background(), "req-7f2c")
_ = log.Info(ctx, "charge succeeded amount=%.2f", 99.00)
```

See [`sdk/go/README.md`](./sdk/go/README.md) for the full API.

**Python**

```python
import loggingsdk

client = loggingsdk.Client("localhost:9092", "billing-service")
client.close()  # at shutdown

token = loggingsdk.log_id_var.set("req-7f2c")
try:
    client.info("charge succeeded amount=%.2f", 99.00)
finally:
    loggingsdk.log_id_var.reset(token)
```

See [`sdk/python/README.md`](./sdk/python/README.md) for the full API.

### 3. Query the logs

```bash
# Health check
curl localhost:8080/healthz

# Filter by correlation id
curl 'localhost:8080/query?logid=req-7f2c&limit=10'

# Filter by project + time range
curl 'localhost:8080/query?project=billing-service&from=2026-09-06T00:00:00Z&limit=50'

# List parquet files (debug)
curl localhost:8080/files
```

Response shape:

```json
{
  "count": 42,
  "results": [
    {
      "timestamp": "2026-09-06T10:23:45.123Z",
      "project":   "billing-service",
      "logid":     "req-7f2c",
      "level":     "INFO",
      "message":   "charge succeeded amount=99.00"
    }
  ]
}
```

## Wire schema

Every Kafka record is a UTF-8 JSON object:

```json
{
  "timestamp": "2026-09-06T10:23:45.123Z",
  "project":   "billing-service",
  "logid":     "req-7f2c-9a31-44e0",
  "level":     "INFO",
  "message":   "charge succeeded"
}
```

- `timestamp` — RFC3339 UTC. Producers fill it in.
- `logid` — application-generated correlation id.
- `level` — `DEBUG | INFO | WARN | ERROR | FATAL`.

## End-to-end smoke test

The repository ships a smoke test that publishes events through the
SDKs and queries them back:

```bash
cd backend/cmd/smoke
go test -tags=integration -run TestEndToEnd -count=1 ./...
```

It expects the backend compose stack to be running.

## Components

| Component             | Language | Role |
|-----------------------|----------|------|
| `backend/`            | Go       | Kafka consumer, Parquet writer, HTTP query API |
| `sdk/go/`             | Go       | Producer SDK (`loggingsdk`) |
| `sdk/python/`         | Python   | Producer SDK (`loggingsdk`) |
| Kafka                 | -        | Single-node KRaft broker |
| Parquet volume        | -        | On-disk rotated files + sidecar indexes |

## Development

```bash
# Backend tests
cd backend && go test ./...

# Go SDK tests
cd sdk/go && go test ./...

# Python SDK tests
cd sdk/python
pip install -e ".[dev]"
python -m pytest -v
```

See the per-component READMEs for the full API surface:

- [`backend/README.md`](./backend/README.md)
- [`sdk/go/README.md`](./sdk/go/README.md)
- [`sdk/python/README.md`](./sdk/python/README.md)