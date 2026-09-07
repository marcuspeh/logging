// Package loggingsdk provides a producer-side client for the centralised
// logging service. It serialises log events to JSON and publishes them to a
// Kafka topic that the logging-backend service consumes (see PLAN §7).
//
// Basic usage:
//
//	log, _ := loggingsdk.New("kafka:9092", "billing-service")
//	defer log.Close()
//	log.Info(ctx, "req-7f2c", "charge succeeded amount=%.2f", 99.00)
//
// By default, calls return synchronously after the Kafka producer
// acknowledges the write. Use WithAsync() to return immediately while a
// background goroutine drains a buffered queue (drop-oldest on overflow).
package loggingsdk

import (
	"log/slog"
	"time"
)

// Option configures a Client.
type Option func(*Client)

// WithAsync enables buffered asynchronous publishing with the given queue
// capacity. When the queue is full, new events drop the oldest entry; the
// drop count is exposed via Client.Stats(). A capacity of 0 is treated as
// "synchronous" (the default).
func WithAsync(capacity int) Option {
	return func(c *Client) {
		c.asyncCapacity = capacity
	}
}

// WithFlushInterval bounds how long an event may sit in the Kafka producer
// before being sent. Defaults to 1s.
func WithFlushInterval(d time.Duration) Option {
	return func(c *Client) {
		c.flushInterval = d
	}
}

// WithMinLevel drops events below the given level client-side. Levels are
// DEBUG < INFO < WARN < ERROR < FATAL. Default is DEBUG (everything passes).
func WithMinLevel(l Level) Option {
	return func(c *Client) {
		c.minLevel = l
	}
}

// WithLogger sets the slog logger used for client-side diagnostics
// (delivery errors, queue overflow, Close timeouts). Default is a no-op
// logger that discards everything.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		c.logger = l
	}
}

// WithBatchSize controls the Kafka producer's max batch size in bytes.
// Defaults to 1 MiB.
func WithBatchSize(n int64) Option {
	return func(c *Client) {
		c.batchSize = n
	}
}
