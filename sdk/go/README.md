# logging-sdk-go

Go producer SDK for the centralised logging service. Serialises log events
to JSON and publishes them to the Kafka topic consumed by the
`logging-backend` service.

Implements PLAN §7.

## Install

```bash
go get github.com/marcuspeh/logging/sdk/go
```

## Usage

The SDK pulls the correlation id from `context.Context`. Attach one at the
top of each request with `WithLogID`:

```go
import loggingsdk "github.com/marcuspeh/logging/sdk/go"

func main() {
    log, err := loggingsdk.New("kafka:9092", "billing-service",
        loggingsdk.WithAsync(4096),
        loggingsdk.WithMinLevel(loggingsdk.LevelInfo),
    )
    if err != nil { /* ... */ }
    defer log.Close()
}

func handleRequest(rawID string) {
    ctx := loggingsdk.WithLogID(context.Background(), rawID)
    _ = log.Info(ctx, "charge succeeded amount=%.2f", 99.00)
    _ = log.Error(ctx, "charge failed: %v", err)
}
```

If `ctx` has no logid attached, the SDK falls back to `"unknown"`.

`project` is used as the Kafka message key so all events for one project
land on the same partition.

## Options

| Option | Default | Purpose |
|--------|---------|---------|
| `WithAsync(n)` | 0 (sync) | Buffered async publishing with capacity `n`; drop-oldest on overflow. |
| `WithFlushInterval(d)` | 1s | Maximum time an event waits in the Kafka producer. |
| `WithBatchSize(n)` | 1 MiB | Maximum batch size in bytes. |
| `WithMinLevel(l)` | DEBUG | Drop events below this level client-side. |
| `WithLogger(l)` | discard | `*slog.Logger` for delivery-error diagnostics. |

## Levels

`LevelDebug`, `LevelInfo`, `LevelWarn`, `LevelError`, `LevelFatal`. Strings
match the wire schema: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`.
`ParseLevel("INFO")` parses config values.

## slog adapter

```go
ctx := loggingsdk.WithLogID(context.Background(), "req-7f2c")
slog.SetDefault(slog.New(loggingsdk.LoggingHandler(log)))
slog.InfoContext(ctx, "user authenticated", "user_id", "alice")
```

The adapter reads the correlation id from `ctx` (set via `WithLogID`).

## Stats

`log.Stats()` returns `Sent`, `Failed`, `Dropped` counters, safe to call
concurrently. Wire them to your metrics pipeline.

## Tests

```bash
go test ./...
```

End-to-end coverage is in
[`backend/cmd/smoke`](../../backend/cmd/smoke) and runs against a live
`logging-backend` compose stack.