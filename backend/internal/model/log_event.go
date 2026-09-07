// Package model defines the on-wire and on-disk log event schema.
//
// The JSON tags here MUST match the Kafka wire schema documented in PLAN §3
// and the Parquet schema in PLAN §5 so the consumer can decode messages from
// the SDKs and the writer can produce compatible Parquet files.
package model

import "time"

// LogEvent is a single log row ingested from Kafka and persisted to Parquet.
type LogEvent struct {
	// Timestamp is the moment the producer created the event.
	// Producers should fill this in; the consumer may backfill if missing.
	Timestamp time.Time `json:"timestamp" parquet:"timestamp"`

	// Project identifies the originating service (e.g. "billing-service").
	// Used as the Kafka partitioning key and as a query filter.
	Project string `json:"project" parquet:"project"`

	// LogID is an application-generated correlation id (request id, trace id).
	LogID string `json:"logid" parquet:"logid"`

	// Level is one of DEBUG | INFO | WARN | ERROR | FATAL.
	Level string `json:"level" parquet:"level"`

	// Message is the free-form log message body.
	Message string `json:"message" parquet:"message"`
}
