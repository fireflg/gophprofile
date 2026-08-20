package main

import (
	"context"
	"log/slog"
	"os/signal"
	"syscall"

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

	appLog := logger.New(cfg.App.Env, cfg.App.LogLevel, tel.LoggerProvider())

	otelx.SetErrorHandler(logger.Component(appLog, "otel"))

	defer func() {
		if shutdownErr := tel.Shutdown(ctx); shutdownErr != nil {
			appLog.Error("shutdown telemetry", slog.Any("error", shutdownErr))
		}
	}()

	go tel.Metrics.Serve(logger.Component(appLog, "metrics"))

	runLog := logger.Component(appLog, "worker")

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
		appLog,
	)
	if err != nil {
		return err
	}

	consumer, err := worker.NewConsumer(cfg.Kafka, processor, appLog)
	if err != nil {
		return err
	}

	runLog.Info("worker started",
		slog.String("topic", cfg.Kafka.Topic), slog.String("group_id", cfg.Kafka.GroupID))

	if err := consumer.Run(ctx); err != nil {
		return err
	}

	runLog.Info("worker stopped")

	return nil
}
