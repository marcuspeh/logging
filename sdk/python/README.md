# logging-sdk

Python producer SDK for the centralised logging service. Serialises log
events to JSON and publishes them to the Kafka topic consumed by the
`logging-backend` service.

Mirrors the Go SDK (`sdk/go`). Implements PLAN §7.

## Install

```bash
pip install -e sdk/python
# or, once published
pip install logging-sdk
```

`confluent-kafka` (which wraps librdkafka) is the only runtime
dependency. Python 3.10+ is required.

## Usage

The SDK reads the correlation id from the module-level
`log_id_var` contextvar. Bind it at the top of each request:

```python
import loggingsdk

client = loggingsdk.Client("kafka:9092", "billing-service",
                           async_capacity=4096,
                           min_level=loggingsdk.Level.INFO)
# ...
client.close()

def handle_request(raw_id: str) -> None:
    token = loggingsdk.log_id_var.set(raw_id)
    try:
        client.info("charge succeeded amount=%.2f", 99.00)
        client.error("charge failed: %s", "insufficient funds")
    finally:
        loggingsdk.log_id_var.reset(token)
```

If `log_id_var` is unset (or holds an empty/non-string value), the SDK
falls back to `"unknown"`.

`project` is used as the Kafka message key so all events for one
project land on the same partition.

## Options

| Argument | Default | Purpose |
|----------|---------|---------|
| `bootstrap` | (required) | Kafka bootstrap servers, e.g. `kafka:9092`. |
| `project` | (required) | Project name; used as the Kafka message key. |
| `topic` | `"logs"` | Kafka topic to publish to. |
| `producer` | `ConfluentProducer(bootstrap)` | Inject a custom `Producer` (testing). |
| `min_level` | `Level.DEBUG` | Drop events below this level client-side. |
| `async_capacity` | `0` (sync) | Buffer `n` events on a worker thread; drop-oldest on overflow. |
| `flush_interval` | `1.0` | Seconds between producer polls in async mode. |

## Levels

`Level.DEBUG`, `Level.INFO`, `Level.WARN`, `Level.ERROR`,
`Level.FATAL`. Strings match the wire schema: `DEBUG`, `INFO`,
`WARN`, `ERROR`, `FATAL`. `ParseLevel("INFO")` parses config values.

## stdlib logging adapter

```python
import logging
import loggingsdk

client = loggingsdk.Client("kafka:9092", "billing-service")
handler = loggingsdk.LoggingHandler(client)
logging.getLogger().addHandler(handler)
logging.getLogger().setLevel(logging.INFO)

# Bind the correlation id so the adapter picks it up automatically.
token = loggingsdk.log_id_var.set("req-7f2c")
try:
    logging.info("user authenticated user=%s", "alice")
finally:
    loggingsdk.log_id_var.reset(token)
```

The adapter reads `log_id_var` at emit time, so any stdlib logger
records the active correlation id without extra plumbing.

## Stats

`client.stats` returns a `Stats` snapshot (`sent`, `failed`,
`dropped`), safe to call concurrently. Wire them to your metrics
pipeline.

## Tests

```bash
cd sdk/python
pip install -e ".[dev]"
python -m pytest -v
```

End-to-end coverage is in
[`backend/cmd/smoke`](../../backend/cmd/smoke) and runs against a live
`logging-backend` compose stack.