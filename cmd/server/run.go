package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/fireflg/gophprofile/internal/api"
	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/handlers"
	"github.com/fireflg/gophprofile/internal/metrics"
	"github.com/fireflg/gophprofile/internal/repository"
	"github.com/fireflg/gophprofile/internal/services"
	"github.com/fireflg/gophprofile/pkg/logger"
	"github.com/fireflg/gophprofile/pkg/otelx"
	"github.com/fireflg/gophprofile/web"
)

// run собирает зависимости, поднимает HTTP-сервер и ждёт сигнала остановки.
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

	runLog := logger.Component(appLog, "server")

	pool, err := repository.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()

	storage, err := services.NewS3Storage(ctx, cfg.S3)
	if err != nil {
		return err
	}

	publisher, err := services.NewKafkaPublisher(cfg.Kafka)
	if err != nil {
		return err
	}
	defer func() { _ = publisher.Close() }()

	repo := repository.NewAvatarRepository(pool)

	avatarService, err := services.NewAvatarService(repo, storage, publisher, cfg.Image, appLog)
	if err != nil {
		return err
	}
	defer avatarService.Wait()

	if err = metrics.RegisterStorageGauge(repo.TotalStorageBytes); err != nil {
		return err
	}

	static, err := web.Static()
	if err != nil {
		return err
	}

	router := api.NewRouter(api.Deps{
		Config:  cfg,
		Logger:  appLog,
		Avatars: handlers.NewAvatarHandler(avatarService, appLog),
		Health: handlers.NewHealthHandler(appLog,
			handlers.Check{Name: "postgres", Probe: repo.Ping},
			handlers.Check{Name: "s3", Probe: storage.Ping},
			handlers.Check{Name: "broker", Probe: publisher.Ping},
		),
		Static: static,
	})

	server, err := api.NewServer(cfg.HTTP, router)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)

	go func() {
		runLog.Info("http server started",
			slog.String("addr", cfg.HTTP.Addr()), slog.String("env", cfg.App.Env))

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		runLog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	runLog.Info("http server stopped")

	return nil
}
