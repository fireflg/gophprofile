// Package services содержит бизнес-логику и адаптеры к внешним системам.
package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
)

// Таймаут отправки файла.
const putObjectTimeout = 5 * time.Second

// S3Storage — адаптер файлового хранилища (S3/MinIO).
type S3Storage struct {
	client        *minio.Client
	bucket        string
	publicBaseURL string
}

// Проверка на этапе компиляции, что адаптер удовлетворяет порту.
var _ domain.FileStorage = (*S3Storage)(nil)

// NewS3Storage создаёт адаптер хранилища.
func NewS3Storage(_ context.Context, cfg config.S3) (*S3Storage, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("s3: endpoint is required")
	}

	if cfg.Bucket == "" {
		return nil, errors.New("s3: bucket is required")
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:    cfg.UseSSL,
		Region:    cfg.Region,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: new client: %w", err)
	}

	return &S3Storage{client: client, bucket: cfg.Bucket, publicBaseURL: cfg.PublicBaseURL}, nil
}

// Put загружает объект в хранилище; size < 0 означает неизвестный размер.
func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, putObjectTimeout)
	defer cancel()

	ctx, span := tracer.Start(ctx, "s3.put", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	span.SetAttributes(
		attribute.String("s3.bucket", s.bucket),
		attribute.String("s3.key", key),
		attribute.Int64("s3.size", size),
		attribute.String("s3.content_type", contentType),
	)

	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return recordError(span, fmt.Errorf("s3: put %s: %w", key, err))
	}

	return nil
}

// Get читает объект из хранилища.
func (s *S3Storage) Get(ctx context.Context, key string) (*domain.Object, error) {
	ctx, span := tracer.Start(ctx, "s3.get", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	span.SetAttributes(
		attribute.String("s3.bucket", s.bucket),
		attribute.String("s3.key", key),
	)

	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, recordError(span, mapGetError(key, err))
	}

	info, err := object.Stat()
	if err != nil {
		_ = object.Close()

		return nil, recordError(span, mapGetError(key, err))
	}

	span.SetAttributes(attribute.Int64("s3.size", info.Size))

	return &domain.Object{
		Body:         object,
		ContentType:  info.ContentType,
		Size:         info.Size,
		ETag:         info.ETag,
		LastModified: info.LastModified,
	}, nil
}

// Delete удаляет объекты по ключам; отсутствующий ключ — не ошибка.
func (s *S3Storage) Delete(ctx context.Context, keys ...string) error {
	ctx, span := tracer.Start(ctx, "s3.delete", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()

	span.SetAttributes(
		attribute.String("s3.bucket", s.bucket),
		attribute.Int("s3.keys", len(keys)),
	)

	for _, key := range keys {
		if key == "" {
			continue
		}

		if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
			if isNotFound(err) {
				continue
			}

			return recordError(span, fmt.Errorf("s3: delete %s: %w", key, err))
		}
	}

	return nil
}

// URL возвращает публичную ссылку на объект.
func (s *S3Storage) URL(key string) string {
	if key == "" || s.publicBaseURL == "" {
		return ""
	}

	return strings.TrimSuffix(s.publicBaseURL, "/") + "/" + key
}

// Ping проверяет доступность хранилища.
func (s *S3Storage) Ping(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("s3: bucket %s: %w", s.bucket, err)
	}

	if !exists {
		return fmt.Errorf("s3: bucket %s does not exist", s.bucket)
	}

	return nil
}

// mapGetError переводит отсутствие объекта в доменную ошибку: обработчики
// уже умеют превращать её в 404, остальное остаётся ошибкой инфраструктуры.
func mapGetError(key string, err error) error {
	if isNotFound(err) {
		return domain.ErrAvatarNotFound
	}

	return fmt.Errorf("s3: get %s: %w", key, err)
}

func isNotFound(err error) bool {
	var response minio.ErrorResponse
	if !errors.As(err, &response) {
		return false
	}

	return response.Code == "NoSuchKey" ||
		(response.Code == "" && response.StatusCode == http.StatusNotFound)
}
