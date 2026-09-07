// Package consumer reads log events from Kafka and hands them to a Sink.
//
// Delivery semantics (PLAN §4): at-least-once. The Kafka offset is committed
// only after the Sink has accepted the event. Decode failures are treated
// as poison pills — logged, committed, and skipped — so one bad row cannot
// stall the consumer.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/marcuspeh/logging-backend/internal/model"
)

// Sink is the minimal contract the consumer needs from a Parquet writer.
type Sink interface {
	Write(ev model.LogEvent) error
}

type config struct {
	brokers []string
	topic   string
	groupID string
}

// Consumer pulls messages from a Kafka topic and forwards decoded events
// to a Sink.
type Consumer struct {
	cfg     config
	sink    Sink
	logger  *slog.Logger
	decoded int64
	skipped int64
}

// Options configures a Consumer.
type Options struct {
	Brokers []string
	Topic   string
	GroupID string
	Logger  *slog.Logger
}

// New builds a Consumer.
func New(opts Options, sink Sink) (*Consumer, error) {
	if len(opts.Brokers) == 0 {
		return nil, errors.New("consumer: at least one broker required")
	}
	if opts.Topic == "" {
		return nil, errors.New("consumer: topic required")
	}
	if opts.GroupID == "" {
		return nil, errors.New("consumer: group id required")
	}
	if sink == nil {
		return nil, errors.New("consumer: sink required")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Consumer{
		cfg: config{
			brokers: append([]string(nil), opts.Brokers...),
			topic:   opts.Topic,
			groupID: opts.GroupID,
		},
		sink:   sink,
		logger: logger,
	}, nil
}

// Run blocks until ctx is cancelled. Returns nil on graceful shutdown;
// non-nil errors for unrecoverable Kafka failures.
func (c *Consumer) Run(ctx context.Context) error {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        c.cfg.brokers,
		Topic:          c.cfg.topic,
		GroupID:        c.cfg.groupID,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		CommitInterval: 0,
		MaxWait:        500 * time.Millisecond,
		StartOffset:    kafka.FirstOffset,
	})
	defer r.Close()

	for {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("consumer: fetch: %w", err)
		}

		ev, decErr := Decode(msg.Value)
		if decErr != nil {
			c.skipped++
			c.logger.Warn("skipping poison-pill message",
				"offset", msg.Offset,
				"partition", msg.Partition,
				"err", decErr,
			)
			if cerr := r.CommitMessages(ctx, msg); cerr != nil &&
				!errors.Is(cerr, context.Canceled) {
				return fmt.Errorf("consumer: commit poison: %w", cerr)
			}
			continue
		}

		if werr := c.sink.Write(ev); werr != nil {
			return fmt.Errorf("consumer: sink write: %w", werr)
		}
		c.decoded++

		if cerr := r.CommitMessages(ctx, msg); cerr != nil {
			if errors.Is(cerr, context.Canceled) {
				return nil
			}
			return fmt.Errorf("consumer: commit: %w", cerr)
		}
	}
}

// Stats returns counters useful for logging / metrics.
func (c *Consumer) Stats() (decoded, skipped int64) {
	return c.decoded, c.skipped
}

// Decode parses a Kafka message body into a LogEvent. Missing timestamps
// are filled with time.Now().UTC(). Required string fields (project, logid,
// level) cause an error when empty.
func Decode(payload []byte) (model.LogEvent, error) {
	var ev model.LogEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return ev, fmt.Errorf("decode json: %w", err)
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if ev.Project == "" {
		return ev, errors.New("decode: project is required")
	}
	if ev.LogID == "" {
		return ev, errors.New("decode: logid is required")
	}
	if ev.Level == "" {
		return ev, errors.New("decode: level is required")
	}
	return ev, nil
}
