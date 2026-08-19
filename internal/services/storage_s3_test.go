package services

import (
	"errors"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
)

func testS3Config() config.S3 {
	return config.S3{
		Endpoint:      "minio:9000",
		AccessKey:     "minioadmin",
		SecretKey:     "minioadmin",
		Bucket:        "avatars",
		PublicBaseURL: "http://localhost:9000/avatars",
	}
}

func TestNewS3StorageRejectsIncompleteConfig(t *testing.T) {
	cfg := testS3Config()
	cfg.Endpoint = ""

	_, err := NewS3Storage(t.Context(), cfg)
	require.ErrorContains(t, err, "endpoint")

	cfg = testS3Config()
	cfg.Bucket = ""

	_, err = NewS3Storage(t.Context(), cfg)
	require.ErrorContains(t, err, "bucket")
}

func TestNewS3StorageRejectsEndpointSchemeConflict(t *testing.T) {
	cfg := testS3Config()
	cfg.Endpoint = "https://minio:9000"
	cfg.UseSSL = false

	_, err := NewS3Storage(t.Context(), cfg)
	require.ErrorContains(t, err, "s3: new client")
}

func TestNewS3StorageAcceptsEndpointWithMatchingScheme(t *testing.T) {
	cfg := testS3Config()
	cfg.Endpoint = "http://minio:9000"

	_, err := NewS3Storage(t.Context(), cfg)
	require.NoError(t, err)
}

func TestNewS3StorageKeepsBucketAndBaseURL(t *testing.T) {
	storage, err := NewS3Storage(t.Context(), testS3Config())
	require.NoError(t, err)

	require.Equal(t, "avatars", storage.bucket)
	require.Equal(t, "http://localhost:9000/avatars", storage.publicBaseURL)
}

func TestS3URL(t *testing.T) {
	storage, err := NewS3Storage(t.Context(), testS3Config())
	require.NoError(t, err)

	require.Equal(t, "http://localhost:9000/avatars/users/1/original.png", storage.URL("users/1/original.png"))
	require.Empty(t, storage.URL(""))

	cfg := testS3Config()
	cfg.PublicBaseURL = "http://localhost:9000/avatars/"

	storage, err = NewS3Storage(t.Context(), cfg)
	require.NoError(t, err)

	require.Equal(t, "http://localhost:9000/avatars/original.png", storage.URL("original.png"))

	cfg.PublicBaseURL = ""

	storage, err = NewS3Storage(t.Context(), cfg)
	require.NoError(t, err)

	require.Empty(t, storage.URL("original.png"))
}

func TestMapGetErrorMapsMissingObject(t *testing.T) {
	missing := minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404, Key: "original.png"}

	require.ErrorIs(t, mapGetError("original.png", missing), domain.ErrAvatarNotFound)
}

func TestMapGetErrorKeepsOtherFailures(t *testing.T) {
	denied := minio.ErrorResponse{Code: "AccessDenied", StatusCode: 403}

	err := mapGetError("original.png", denied)

	require.ErrorIs(t, err, denied)
	require.NotErrorIs(t, err, domain.ErrAvatarNotFound)
	require.ErrorContains(t, err, "original.png")
}

func TestIsNotFound(t *testing.T) {
	require.True(t, isNotFound(minio.ErrorResponse{Code: "NoSuchKey"}))
	require.True(t, isNotFound(minio.ErrorResponse{StatusCode: 404}))
	require.False(t, isNotFound(minio.ErrorResponse{Code: "NoSuchBucket", StatusCode: 404}))
	require.False(t, isNotFound(errors.New("connection refused")))
}
