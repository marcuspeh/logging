// Command logging-backend is the entry point for the centralised logging
// service.
//
// It wires together the Kafka consumer, the Parquet writer, and the HTTP
// query API (PLAN §4, §6).
//
// Lifecycle:
//
//  1. Load configuration from the environment.
//  2. Build the Parquet writer (opens the first file).
//  3. Build the Kafka consumer over the writer.
//  4. Start a background goroutine that periodically reloads the Parquet
//     index so newly-rotated files appear in /query results.
//  5. Start the HTTP query API.
//  6. Block on SIGINT / SIGTERM, then shut down: stop the HTTP server,
//     cancel the consumer context, close the Parquet writer, exit.
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
	)

	// Parquet writer (Task 3).
	pw, err := parquet.New(cfg.ParquetDir, cfg.ParquetRotateBytes)
	if err != nil {
		return err
	}

	// Kafka consumer (Task 4).
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

	// Index loader (Task 5) — backs the HTTP API.
	loader, err := api.NewIndexLoader(cfg.ParquetDir)
	if err != nil {
		_ = pw.Close()
		return err
	}

	// HTTP query API (Task 5).
	srv := api.NewServer(loader, logger.With("component", "api"))

	// Root context cancelled on SIGINT/SIGTERM.
	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Periodic index reload so freshly-sealed Parquet files appear in
	// query results without a service restart.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reloadLoop(rootCtx, loader, logger)
	}()

	// Consumer goroutine.
	wg.Add(1)
	consumerErrCh := make(chan error, 1)
	go func() {
		defer wg.Done()
		if err := c.Run(rootCtx); err != nil {
			consumerErrCh <- err
		}
		close(consumerErrCh)
	}()

	// HTTP server (blocks until ctx done).
	srvErr := srv.Run(rootCtx, cfg.HTTPAddr, cfg.ShutdownTimeout)

	// Cancellation has fired (or HTTP server failed) — wait for
	// background goroutines to drain.
	wg.Wait()

	// Close the Parquet writer last so the consumer goroutine has
	// finished using it. Ignore the error: if the writer was already
	// closed this returns nil.
	if err := pw.Close(); err != nil {
		logger.Warn("closing parquet writer", "err", err)
	}

	// Surface consumer errors if any.
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
// until ctx is cancelled. A Reload failure is logged but not fatal — the
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
