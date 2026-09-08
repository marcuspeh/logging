# logging-sdk-go

Go producer SDK for the centralised logging service. Serialises log
events to JSON and publishes them to the Kafka topic consumed by the
`logging-backend` service.

Implements PLAN §7.

## Install

```bash
go get github.com/marcuspeh/logging/sdk/go
```

## Quick start

```go
import (
    "context"
    "log"

    loggingsdk "github.com/marcuspeh/logging/sdk/go"
)

func main() {
    log, err := loggingsdk.New("kafka:9092", "billing-service",
        loggingsdk.WithAsync(4096),                    // buffer + drop-oldest
        loggingsdk.WithMinLevel(loggingsdk.LevelInfo), // client-side filter
    )
    if err != nil {
        log.Fatal(err)
    }
    defer log.Close()
}

func handleRequest(rawID string) {
    // Attach the correlation id once at the top of the request.
    ctx := loggingsdk.WithLogID(context.Background(), rawID)

    _ = log.Info(ctx, "charge succeeded amount=%.2f", 99.00)
    _ = log.Error(ctx, "charge failed: %v", "insufficient funds")
}
```

If `ctx` has no logid attached, the SDK falls back to `"-"`.

`project` is used as the Kafka message key so all events for one
project land on the same partition.

## Public API

```go
// Construction
func New(bootstrap, project string, opts ...Option) (*Client, error)

// Sending
func (c *Client) Log (ctx context.Context, level Level, format string, args ...any) error
func (c *Client) Debug(ctx context.Context, format string, args ...any) error
func (c *Client) Info (ctx context.Context, format string, args ...any) error
func (c *Client) Warn (ctx context.Context, format string, args ...any) error
func (c *Client) Error(ctx context.Context, format string, args ...any) error
func (c *Client) Fatal(ctx context.Context, format string, args ...any) error

// Lifecycle
func (c *Client) Close() error

// Introspection
func (c *Client) Project() string
func (c *Client) Stats()  Stats  // Sent, Failed, Dropped
```

The format string is passed to `fmt.Sprintf`, so use `%.2f`, `%v`, etc.
just like `log.Printf`. If you don't need formatting, pass a plain
string with no args.

## Options

| Option | Default | Purpose |
|--------|---------|---------|
| `WithAsync(n)` | `0` (sync) | Buffered async publishing with capacity `n`; drop-oldest on overflow. |
| `WithFlushInterval(d)` | `1s` | Maximum time an event waits in the Kafka producer. |
| `WithBatchSize(n)` | `1 MiB` | Maximum batch size in bytes. |
| `WithMinLevel(l)` | `LevelDebug` | Drop events below this level client-side. |
| `WithLogger(l)` | discard | `*slog.Logger` for delivery-error diagnostics. |

## Levels

`LevelDebug`, `LevelInfo`, `LevelWarn`, `LevelError`, `LevelFatal`.
Strings match the wire schema: `DEBUG`, `INFO`, `WARN`, `ERROR`,
`FATAL`. `ParseLevel("INFO")` parses config values.

## Correlation id

`WithLogID(ctx, id)` attaches the id under `loggingsdk.LogIDKey`
(`"log_id"`). The SDK reads it on every send, so the same pattern
works in HTTP handlers, gRPC interceptors, queue consumers, etc.:

```go
// net/http middleware
func withLogID(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        id := r.Header.Get("X-Request-ID")
        ctx := loggingsdk.WithLogID(r.Context(), id)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

If you don't set one, `logidFromCtx` returns `"-"` — searchable in the
query API but obviously less useful.

## slog adapter

Replace the default slog handler so any `slog.InfoContext(ctx, ...)`
call in your code lands in Kafka:

```go
ctx := loggingsdk.WithLogID(context.Background(), "req-7f2c")
slog.SetDefault(slog.New(loggingsdk.LoggingHandler(log)))
slog.InfoContext(ctx, "user authenticated", "user_id", "alice")
```

The adapter reads the correlation id from `ctx` (set via `WithLogID`).

## Stats

`log.Stats()` returns `Sent`, `Failed`, `Dropped` counters, safe to
call concurrently. Wire them to your metrics pipeline:

```go
go func() {
    t := time.NewTicker(15 * time.Second)
    defer t.Stop()
    for range t.C {
        s := log.Stats()
        metricsSent.WithLabelValues("kafka").Set(float64(s.Sent))
        metricsDropped.WithLabelValues("kafka").Set(float64(s.Dropped))
        metricsFailed.WithLabelValues("kafka").Set(float64(s.Failed))
    }
}()
```

## Tests

```bash
go test ./...
```

End-to-end coverage is in
[`backend/cmd/smoke`](../../backend/cmd/smoke) and runs against a
live `logging-backend` compose stack.