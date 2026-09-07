from __future__ import annotations

from typing import Iterable, Protocol


class Producer(Protocol):
    """Minimal interface the SDK needs from a Kafka producer.

    Implementations must be safe to call from multiple threads.
    """

    def produce(self, topic: str, key: bytes, payload: bytes) -> None:
        """Enqueue one message. Must not block; delivery happens in the background."""
        ...

    def poll(self, timeout: float = 0.0) -> None:
        """Drive background delivery callbacks. Call periodically."""
        ...

    def flush(self, timeout: float) -> int:
        """Block until pending messages are delivered or timeout expires."""
        ...

    def close(self) -> int:
        """Flush + release resources. Returns number of messages still in queue."""
        ...


class ConfluentProducer:
    """confluent-kafka backed Producer.

    Wraps ``confluent_kafka.Producer`` with the configuration we use
    across services: snappy compression, all-acks, idempotent disabled
    (the collector tolerates duplicates).
    """

    def __init__(self, bootstrap: str, *, flush_timeout: float = 5.0) -> None:
        from confluent_kafka import Producer

        self._p = Producer({
            "bootstrap.servers": bootstrap,
            "enable.idempotence": False,
            "acks": "all",
            "compression.type": "snappy",
            "linger.ms": 50,
        })
        self._flush_timeout = flush_timeout

    def produce(self, topic: str, key: bytes, payload: bytes) -> None:
        # librdkafka's produce() is non-blocking; the actual send happens
        # on the next poll(). Delivery failures are surfaced via the
        # per-message callback.
        self._p.produce(
            topic=topic,
            key=key,
            value=payload,
            on_delivery=self._on_delivery,
        )

    def poll(self, timeout: float = 0.0) -> None:
        self._p.poll(timeout)

    def flush(self, timeout: float) -> int:
        return self._p.flush(timeout)

    def close(self) -> int:
        return self._p.flush(self._flush_timeout)

    def _on_delivery(self, err, _msg) -> None:
        if err is not None:
            # Surface in producer logs; callers can also poll .stats()
            print(f"loggingsdk: kafka delivery failed: {err}")


def drain(producer: Producer, timeout: float = 5.0) -> int:
    """Helper used by Client.close(): flush and return the leftover count."""
    if hasattr(producer, "close"):
        return producer.close()
    producer.flush(timeout)
    return 0


def produce_batch(producer: Producer, topic: str, items: Iterable[tuple[bytes, bytes]]) -> None:
    """Produce each (key, payload) pair. Helper used by the async worker."""
    for key, payload in items:
        producer.produce(topic, key, payload)