package model

import "time"

// LogEvent is a single log row ingested from Kafka and persisted to Parquet.
// JSON tags match the wire schema (PLAN §3); parquet tags match the on-disk
// schema (PLAN §5).
type LogEvent struct {
	Timestamp time.Time `json:"timestamp" parquet:"timestamp"`
	Project   string    `json:"project" parquet:"project"`
	LogID     string    `json:"logid" parquet:"logid"`
	Level     string    `json:"level" parquet:"level"`
	Message   string    `json:"message" parquet:"message"`
}
