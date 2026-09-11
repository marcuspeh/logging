// Command logging-backend is the entry point for the centralised logging
// service. It wires together the Kafka consumer, the Parquet writer, and
// the HTTP query API (PLAN §4, §6).
//
// Lifecycle: load config → build writer/consumer/api → start consumer +
// index-reload goroutines → run HTTP server → on SIGINT/SIGTERM, drain,
// close writer, exit.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/marcuspeh/logging-backend/internal/api"
	"github.com/marcuspeh/logging-backend/internal/config"
	"github.com/marcuspeh/logging-backend/internal/consumer"
	"github.com/marcuspeh/logging-backend/internal/parquet"
)

const indexReloadInterval = 30 * time.Second

// retentionLoop deletes Parquet files older than cfg.Retention every
// cfg.RetentionInterval. A sweep failure is logged but not fatal.
func retentionLoop(ctx context.Context, pw *parquet.Writer, ttl, interval time.Duration, logger *slog.Logger) {
	if ttl <= 0 || interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			res, err := pw.SweepRetention(time.Now(), ttl)
			if err != nil {
				logger.Warn("retention sweep failed", "err", err)
				continue
			}
			if len(res.Deleted) > 0 {
				logger.Info("retention sweep",
					"deleted", len(res.Deleted),
					"scanned", res.Scanned,
					"bytes_freed", res.BytesFreed,
				)
			}
		}
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	if err := run(logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.Info("starting logging-backend",
		"kafka_brokers", cfg.KafkaBrokers,
		"kafka_topic", cfg.KafkaTopic,
		"kafka_group_id", cfg.KafkaGroupID,
		"parquet_dir", cfg.ParquetDir,
		"parquet_rotate_bytes", cfg.ParquetRotateBytes,
		"http_addr", cfg.HTTPAddr,
		"shutdown_timeout", cfg.ShutdownTimeout,
		"retention", cfg.Retention,
		"retention_interval", cfg.RetentionInterval,
	)

	pw, err := parquet.New(cfg.ParquetDir, parquet.FlushOptions{
		RotateBytes: cfg.ParquetRotateBytes,
		RotateEvery: cfg.ParquetRotateEvery,
		FlushRows:   cfg.ParquetFlushRows,
		FlushEvery:  cfg.ParquetFlushEvery,
	})
	if err != nil {
		return err
	}

	c, err := consumer.New(consumer.Options{
		Brokers: cfg.KafkaBrokers,
		Topic:   cfg.KafkaTopic,
		GroupID: cfg.KafkaGroupID,
		Logger:  logger.With("component", "consumer"),
	}, pw)
	if err != nil {
		_ = pw.Close()
		return err
	}

	loader, err := api.NewIndexLoader(cfg.ParquetDir)
	if err != nil {
		_ = pw.Close()
		return err
	}

	srv := api.NewServer(loader, logger.With("component", "api"))

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reloadLoop(rootCtx, loader, logger)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		retentionLoop(rootCtx, pw, cfg.Retention, cfg.RetentionInterval, logger)
	}()

	wg.Add(1)
	consumerErrCh := make(chan error, 1)
	go func() {
		defer wg.Done()
		if err := c.Run(rootCtx); err != nil {
			consumerErrCh <- err
		}
		close(consumerErrCh)
	}()

	srvErr := srv.Run(rootCtx, cfg.HTTPAddr, cfg.ShutdownTimeout)

	wg.Wait()

	if err := pw.Close(); err != nil {
		logger.Warn("closing parquet writer", "err", err)
	}

	if cErr, ok := <-consumerErrCh; ok && cErr != nil {
		if errors.Is(cErr, context.Canceled) {
			logger.Info("consumer stopped")
		} else {
			logger.Error("consumer failed", "err", cErr)
			if srvErr == nil {
				srvErr = cErr
			}
		}
	}

	if srvErr != nil && !errors.Is(srvErr, context.Canceled) {
		return srvErr
	}
	logger.Info("logging-backend stopped cleanly")
	return nil
}

// reloadLoop refreshes the in-memory Parquet index every indexReloadInterval
// until ctx is cancelled. A reload failure is logged but not fatal — the
// previous index keeps serving queries.
func reloadLoop(ctx context.Context, loader *api.IndexLoader, logger *slog.Logger) {
	t := time.NewTicker(indexReloadInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := loader.Reload(); err != nil {
				logger.Warn("index reload failed", "err", err)
			}
		}
	}
}
