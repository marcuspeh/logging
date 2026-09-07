# logging-backend

Centralised logging collector: consumes log events from Kafka, persists them
to rotated Parquet files on local disk, and serves them via an HTTP query
API.

Implements PLAN §4, §5, §6.

## Layout

```
backend/
├── main.go                  # entry point — wires consumer + writer + api
├── cmd/smoke/               # end-to-end test (Task 8)
├── internal/
│   ├── config/              # env-driven configuration
│   ├── model/               # LogEvent schema (wire + parquet)
│   ├── parquet/             # rotating Parquet writer + index sidecar
│   ├── consumer/            # Kafka consumer (at-least-once, poison-pill safe)
│   └── api/                 # HTTP query API (index loader + query engine + server)
├── Dockerfile
├── docker-compose.yml       # Kafka + collector stack
└── go.mod
```

## Run with docker-compose

```bash
# one-time
mkdir -p /home/marcuspeh/data/logging-system/kafka \
         /home/marcuspeh/data/logging-system/parquet
# edit docker-compose.yml if you want different paths

cd backend
docker compose up -d

# smoke test (sends 1000 synthetic events, asserts API results)
go run ./cmd/smoke -api http://localhost:4665

docker compose down        # keeps /home/marcuspeh/data/logging-system intact
```

The collector listens on container port `8080`; the compose file
maps host port `4665` to it. Override either side by editing
`docker-compose.yml`.

## Endpoints

- `GET /healthz` → `{"status":"ok"}`
- `GET /query?project=&logid=&from=&to=&level=&limit=&order=` — requires
  at least one of `project` or `logid`. `from`/`to` are RFC3339.
- `GET /files` — manifest of every Parquet file the API can see.

## Environment variables

| Var | Default |
|-----|---------|
| `KAFKA_BROKERS` | `localhost:9092` |
| `KAFKA_TOPIC` | `logs` |
| `KAFKA_GROUP_ID` | `logging-collector` |
| `PARQUET_DIR` | `/data/parquet` |
| `PARQUET_ROTATE_BYTES` | `134217728` (128 MiB) |
| `HTTP_ADDR` | `:8080` |
| `SHUTDOWN_TIMEOUT` | `10s` |
| `RETENTION` | `336h` (14 days) |
| `RETENTION_INTERVAL` | `1h` |

## Retention

The collector sweeps the Parquet directory every `RETENTION_INTERVAL`
and removes any sealed file whose last event timestamp (read from the
sidecar `*.idx.json`) is older than `RETENTION`. The active (currently
open) file is never removed.

Set `RETENTION=0` to disable deletion entirely (sweep still runs but
deletes nothing).

Corrupt or missing sidecar files are skipped (never deleted), so a
single bad index can't take out live data.

## Parquet file layout

File naming: `logs-<unix-seconds>-<seq>.parquet`.

Each file has a sidecar `<file>.parquet.idx.json` with:

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

The query API uses these sidecars to skip files whose time/project range
can't intersect the query.

## Delivery semantics

At-least-once. The Kafka offset is committed only after the Parquet writer
has accepted the event. Decode failures (bad JSON / missing required
fields) are logged, committed, and skipped so one bad row cannot stall
the consumer.

## Tests

```bash
go test ./...                 # all unit tests
go run ./cmd/smoke            # end-to-end against a running docker-compose stack
```