package loggingsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
)

// LogIDKey is the context.Context key used to store the correlation id
// (mirrors go-tools/logger.LogIDKey so callers don't need the dep).
// Use WithLogID to attach one.
const LogIDKey = "log_id"

// WithLogID returns ctx carrying the given correlation id. The SDK pulls
// this value out of ctx on every send.
func WithLogID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, LogIDKey, id)
}

// Client publishes log events to Kafka.
type Client struct {
	project string
	writer  *kafka.Writer

	asyncCapacity int
	queue         chan asyncItem
	workerWG      sync.WaitGroup
	workerStop    chan struct{}

	flushInterval time.Duration
	minLevel      Level
	logger        *slog.Logger
	batchSize     int64

	dropped atomic.Int64
	sent    atomic.Int64
	failed  atomic.Int64
}

type asyncItem struct {
	topic   string
	key     []byte
	payload []byte
}

// Stats reports client-side counters. Safe to call concurrently.
type Stats struct {
	Sent    int64
	Failed  int64
	Dropped int64
}

func logidFromCtx(ctx context.Context) string {
	if v := ctx.Value(LogIDKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "-"
}

func New(bootstrap, project string, opts ...Option) (*Client, error) {
	if bootstrap == "" {
		return nil, errors.New("loggingsdk: bootstrap is required")
	}
	if project == "" {
		return nil, errors.New("loggingsdk: project is required")
	}

	c := &Client{
		project:       project,
		flushInterval: time.Second,
		minLevel:      LevelDebug,
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		batchSize:     1 << 20,
		asyncCapacity: 0,
	}
	for _, opt := range opts {
		opt(c)
	}

	c.writer = &kafka.Writer{
		Addr:                   kafka.TCP(bootstrap),
		Topic:                  "logs",
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		BatchTimeout:           c.flushInterval,
		BatchBytes:             c.batchSize,
		AllowAutoTopicCreation: true,
		Compression:            kafka.Snappy,
	}

	if c.asyncCapacity > 0 {
		c.queue = make(chan asyncItem, c.asyncCapacity)
		c.workerStop = make(chan struct{})
		c.workerWG.Add(1)
		go c.asyncWorker()
	}

	return c, nil
}

func (c *Client) Project() string { return c.project }

func (c *Client) Stats() Stats {
	return Stats{
		Sent:    c.sent.Load(),
		Failed:  c.failed.Load(),
		Dropped: c.dropped.Load(),
	}
}

func (c *Client) Log(ctx context.Context, level Level, message string, args ...any) error {
	if level < c.minLevel {
		return nil
	}

	payload, err := encodeEvent(event{
		Timestamp: time.Now().UTC(),
		Project:   c.project,
		LogID:     logidFromCtx(ctx),
		Level:     level.String(),
		Message:   formatMessage(message, args),
	})
	if err != nil {
		return fmt.Errorf("loggingsdk: encode: %w", err)
	}

	return c.publish(ctx, "logs", []byte(c.project), payload)
}

func (c *Client) Debug(ctx context.Context, format string, args ...any) {
	c.Log(ctx, LevelDebug, format, args...)
}

func (c *Client) Info(ctx context.Context, format string, args ...any) {
	c.Log(ctx, LevelInfo, format, args...)
}

func (c *Client) Warn(ctx context.Context, format string, args ...any) {
	c.Log(ctx, LevelWarn, format, args...)
}

func (c *Client) Error(ctx context.Context, format string, args ...any) {
	c.Log(ctx, LevelError, format, args...)
}

func (c *Client) Fatal(ctx context.Context, format string, args ...any) {
	c.Log(ctx, LevelFatal, format, args...)
}

func (c *Client) Close() error {
	if c.writer == nil {
		return nil
	}
	if c.asyncCapacity > 0 {
		close(c.workerStop)
		c.workerWG.Wait()
	}
	err := c.writer.Close()
	c.writer = nil
	return err
}

func (c *Client) publish(ctx context.Context, topic string, key, payload []byte) error {
	if c.asyncCapacity > 0 {
		item := asyncItem{topic: topic, key: key, payload: payload}
		select {
		case c.queue <- item:
			return nil
		default:
			c.dropped.Add(1)
			select {
			case <-c.queue:
				c.dropped.Add(1)
			default:
			}
			select {
			case c.queue <- item:
				return nil
			default:
				return errors.New("loggingsdk: queue full")
			}
		}
	}

	msg := kafka.Message{Topic: topic, Key: key, Value: payload}
	if err := c.writer.WriteMessages(ctx, msg); err != nil {
		c.failed.Add(1)
		c.logger.Warn("kafka write failed", "err", err, "project", c.project)
		return err
	}
	c.sent.Add(1)
	return nil
}

func (c *Client) asyncWorker() {
	defer c.workerWG.Done()
	for {
		select {
		case <-c.workerStop:
			for {
				select {
				case item := <-c.queue:
					c.sendSync(item)
				default:
					return
				}
			}
		case item := <-c.queue:
			c.sendSync(item)
		}
	}
}

func (c *Client) sendSync(item asyncItem) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.writer.WriteMessages(ctx, kafka.Message{Topic: item.topic, Key: item.key, Value: item.payload}); err != nil {
		c.failed.Add(1)
		c.logger.Warn("async kafka write failed", "err", err)
		return
	}
	c.sent.Add(1)
}

func formatMessage(format string, args []any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

type event struct {
	Timestamp time.Time `json:"timestamp"`
	Project   string    `json:"project"`
	LogID     string    `json:"logid"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

func encodeEvent(e event) ([]byte, error) { return json.Marshal(e) }

// LoggingHandler returns a slog.Handler that publishes every record via c.
// The correlation id is read from ctx (set by WithLogID).
func LoggingHandler(c *Client) slog.Handler {
	return &slogAdapter{c: c}
}

type slogAdapter struct{ c *Client }

func (a *slogAdapter) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (a *slogAdapter) Handle(ctx context.Context, r slog.Record) error {
	level := LevelInfo
	switch {
	case r.Level >= slog.LevelError:
		level = LevelError
	case r.Level >= slog.LevelWarn:
		level = LevelWarn
	case r.Level >= slog.LevelInfo:
		level = LevelInfo
	default:
		level = LevelDebug
	}
	return a.c.Log(ctx, level, r.Message)
}
func (a *slogAdapter) WithAttrs(_ []slog.Attr) slog.Handler { return a }
func (a *slogAdapter) WithGroup(_ string) slog.Handler      { return a }
