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
	"github.com/marcuspeh/logging-backend/internal/configstore"
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
			} else if res.Scanned > 0 {
				logger.Debug("retention sweep: nothing to delete",
					"scanned", res.Scanned,
					"skipped", res.Skipped,
				)
			}
		}
	}
}

// compactionLoop merges small sealed Parquet files into bigger ones
// every interval. A failed compaction is logged but not fatal — the
// next tick retries. The writer's active file is never a source.
func compactionLoop(ctx context.Context, pw *parquet.Writer, compactor *parquet.Compactor, interval time.Duration, logger *slog.Logger) {
	if compactor == nil || interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			res, err := compactor.Compact(pw.ActiveBaseName, pw.RotateBytes())
			if err != nil {
				logger.Warn("compaction failed", "err", err)
				continue
			}
			if res.Merged > 0 {
				logger.Info("compaction sweep",
					"merged", res.Merged,
					"scanned", res.Scanned,
					"skipped", res.Skipped,
					"rows", res.RowsCompacted,
					"bytes_freed", res.BytesFreed,
				)
			} else if res.Scanned > 0 {
				logger.Debug("compaction sweep: nothing to merge",
					"scanned", res.Scanned,
					"skipped", res.Skipped,
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
		"compaction_interval", cfg.CompactionInterval,
		"compaction_max_file_bytes", cfg.CompactionMaxFileBytes,
		"config_store_url", cfg.ConfigStoreURL,
		"config_store_project", cfg.ConfigStoreProject,
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

	loader, err := api.NewIndexLoader(cfg.ParquetDir, logger.With("component", "api"))
	if err != nil {
		_ = pw.Close()
		return err
	}

	projects := configstore.NewProjectsProviderFromConfig(
		cfg.ConfigStoreURL,
		cfg.ConfigStoreProject,
		logger.With("component", "configstore"),
	)

	srv := api.NewServer(loader, projects, logger.With("component", "api"))

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reloadLoop(rootCtx, loader, logger)
	}()

	// Periodic flush: the writer's flush-by-time check only fires from
	// inside Write(). When traffic is sparse (e.g. <1024 events per
	// flushRows batch) the active parquet file never flushes on its own,
	// so the API can't see recent rows until either the next write or
	// rotation. Ticking every flushEvery makes the writer commit pending
	// rows regardless of whether new events are arriving.
	if cfg.ParquetFlushEvery > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			flushLoop(rootCtx, pw, cfg.ParquetFlushEvery, logger)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		retentionLoop(rootCtx, pw, cfg.Retention, cfg.RetentionInterval, logger)
	}()

	compactor := parquet.NewCompactor(cfg.ParquetDir, cfg.CompactionMaxFileBytes)
	wg.Add(1)
	go func() {
		defer wg.Done()
		compactionLoop(rootCtx, pw, compactor, cfg.CompactionInterval, logger)
	}()

	wg.Add(1)
	consumerErrCh := make(chan error, 1)
	go func() {
		defer wg.Done()
		// Always log the consumer's exit, even when Run returns nil
		// (clean shutdown). Silent exit on a goroutine whose error
		// channel is buffered + nil is exactly how a Kafka partition
		// loss or bad credentials can take down ingestion without any
		// breadcrumb. See incident: consumer silently exited after the
		// storage move and no logs were written.
		err := c.Run(rootCtx)
		switch {
		case err == nil:
			logger.Info("consumer stopped", "reason", "run returned nil")
		case errors.Is(err, context.Canceled):
			logger.Info("consumer stopped", "reason", "context canceled")
		default:
			logger.Error("consumer stopped", "err", err)
		}
		consumerErrCh <- err
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

// flushLoop calls pw.Flush() every interval until ctx is cancelled. This
// is the periodic counterpart to the flushBySize / flushByTime check that
// lives inside Write() — without this loop, sparse traffic would leave
// rows buffered in memory until either 1024 events accumulated or the
// file rotated, leaving recent logs invisible to /query.
func flushLoop(ctx context.Context, pw *parquet.Writer, interval time.Duration, logger *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := pw.Flush(); err != nil {
				logger.Warn("periodic parquet flush failed", "err", err)
			}
		}
	}
}
