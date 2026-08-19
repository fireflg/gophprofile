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
)

// run собирает зависимости воркера и читает топик до сигнала остановки.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	zapLog, err := logger.New(cfg.App.Env, cfg.App.LogLevel)
	if err != nil {
		return err
	}
	defer func() { _ = zapLog.Sync() }()

	runLog := logger.Component(zapLog, "worker")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
