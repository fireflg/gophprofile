package main

import (
	"context"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/repository"
	"github.com/fireflg/gophprofile/internal/services"
	"github.com/fireflg/gophprofile/internal/worker"
	"github.com/fireflg/gophprofile/pkg/logger"
	"github.com/fireflg/gophprofile/pkg/otelx"
)

// run собирает зависимости воркера и читает топик до сигнала остановки.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := otelx.Setup(ctx, cfg.OTel, cfg.Metrics)
	if err != nil {
		return err
	}

	zapLog, err := logger.New(cfg.App.Env, cfg.App.LogLevel, tel.LoggerProvider())
	if err != nil {
		return err
	}

	otelx.SetErrorHandler(zapLog)

	defer func() {
		if shutdownErr := tel.Shutdown(ctx); shutdownErr != nil {
			zapLog.Error("shutdown telemetry", zap.Error(shutdownErr))
		}

		_ = zapLog.Sync()
	}()

	go tel.Metrics.Serve(zapLog)

	runLog := logger.Component(zapLog, "worker")

	pool, err := repository.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()

	storage, err := services.NewS3Storage(ctx, cfg.S3)
	if err != nil {
		return err
	}

	processor, err := worker.NewProcessor(
		repository.NewAvatarRepository(pool),
		storage,
		cfg.Image.ThumbnailSizes,
		zapLog,
	)
	if err != nil {
		return err
	}

	consumer, err := worker.NewConsumer(cfg.Kafka, processor, zapLog)
	if err != nil {
		return err
	}

	runLog.Info("worker started",
		zap.String("topic", cfg.Kafka.Topic), zap.String("group_id", cfg.Kafka.GroupID))

	if err := consumer.Run(ctx); err != nil {
		return err
	}

	runLog.Info("worker stopped")

	return nil
}
