//go:build docker

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/fireflg/gophprofile/internal/config"
	"github.com/fireflg/gophprofile/internal/domain"
	"github.com/fireflg/gophprofile/internal/repository"
)

var (
	testPool *pgxpool.Pool
	testDSN  string
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		code, err := runWithDSN(ctx, m, dsn)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)

			os.Exit(1)
		}

		os.Exit(code)
	}

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("avatars"),
		tcpostgres.WithUsername("avatars"),
		tcpostgres.WithPassword("avatars"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start postgres: %v\n", err)

		os.Exit(1)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err == nil {
		var code int

		code, err = runWithDSN(ctx, m, dsn)
		if err == nil {
			terminate(container)

			os.Exit(code)
		}
	}

	fmt.Fprintln(os.Stderr, err)
	terminate(container)

	os.Exit(1)
}

func terminate(container *tcpostgres.PostgresContainer) {
	if err := testcontainers.TerminateContainer(container); err != nil {
		fmt.Fprintf(os.Stderr, "terminate container: %v\n", err)
	}
}

func runWithDSN(ctx context.Context, m *testing.M, dsn string) (int, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("create pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return 0, fmt.Errorf("ping postgres: %w", err)
	}

	if err := applyMigrations(ctx, pool); err != nil {
		return 0, err
	}

	testPool = pool
	testDSN = dsn

	return m.Run(), nil
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		return fmt.Errorf("glob migrations: %w", err)
	}

	if len(files) == 0 {
		return errors.New("no migrations found")
	}

	sort.Strings(files)

	reset := []string{
		"DROP TABLE IF EXISTS avatars CASCADE",
		"DROP TYPE IF EXISTS avatar_upload_status",
		"DROP TYPE IF EXISTS avatar_processing_status",
	}

	for _, statement := range reset {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("reset schema: %w", err)
		}
	}

	for _, file := range files {
		statements, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", file, err)
		}

		if _, err := pool.Exec(ctx, string(statements)); err != nil {
			return fmt.Errorf("apply %s: %w", file, err)
		}
	}

	return nil
}

func newRepo(t *testing.T) *repository.AvatarRepository {
	t.Helper()

	_, err := testPool.Exec(t.Context(), "TRUNCATE avatars")
	require.NoError(t, err)

	return repository.NewAvatarRepository(testPool)
}

func newAvatar(userID string) *domain.Avatar {
	id := uuid.New()

	return &domain.Avatar{
		ID:               id,
		UserID:           userID,
		FileName:         "avatar.png",
		MimeType:         "image/png",
		SizeBytes:        2048,
		S3Key:            "avatars/" + userID + "/" + id.String() + "/original.png",
		UploadStatus:     domain.UploadStatusUploaded,
		ProcessingStatus: domain.ProcessingStatusPending,
	}
}

func createAvatar(t *testing.T, repo *repository.AvatarRepository, userID string) *domain.Avatar {
	t.Helper()

	avatar := newAvatar(userID)
	require.NoError(t, repo.Create(t.Context(), avatar))

	return avatar
}

func shiftCreatedAt(t *testing.T, id uuid.UUID, delta time.Duration) {
	t.Helper()

	_, err := testPool.Exec(t.Context(),
		"UPDATE avatars SET created_at = created_at + $2 WHERE id = $1", id, delta)
	require.NoError(t, err)
}

func TestCreateAndGetByID(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")

	require.False(t, avatar.CreatedAt.IsZero())
	require.False(t, avatar.UpdatedAt.IsZero())

	stored, err := repo.GetByID(t.Context(), avatar.ID)
	require.NoError(t, err)

	require.Equal(t, avatar.ID, stored.ID)
	require.Equal(t, "user-1", stored.UserID)
	require.Equal(t, "avatar.png", stored.FileName)
	require.Equal(t, "image/png", stored.MimeType)
	require.Equal(t, int64(2048), stored.SizeBytes)
	require.Equal(t, avatar.S3Key, stored.S3Key)
	require.Equal(t, domain.UploadStatusUploaded, stored.UploadStatus)
	require.Equal(t, domain.ProcessingStatusPending, stored.ProcessingStatus)
	require.NotNil(t, stored.ThumbnailS3Keys)
	require.Empty(t, stored.ThumbnailS3Keys)
	require.Zero(t, stored.Width)
	require.Zero(t, stored.Height)
	require.Nil(t, stored.DeletedAt)
}

func TestStatusColumnsRejectUnknownValues(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")

	_, err := testPool.Exec(t.Context(),
		"UPDATE avatars SET upload_status = 'bogus' WHERE id = $1", avatar.ID)
	require.Error(t, err)

	_, err = testPool.Exec(t.Context(),
		"UPDATE avatars SET processing_status = 'bogus' WHERE id = $1", avatar.ID)
	require.Error(t, err)
}

func TestNotNullColumnsRejectNull(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")

	columns := []string{"upload_status", "processing_status", "thumbnail_s3_keys", "created_at", "updated_at"}

	for _, column := range columns {
		t.Run(column, func(t *testing.T) {
			_, err := testPool.Exec(t.Context(),
				"UPDATE avatars SET "+column+" = NULL WHERE id = $1", avatar.ID)
			require.Error(t, err)
		})
	}
}

func TestColumnDefaultsApplyOnInsert(t *testing.T) {
	repo := newRepo(t)

	id := uuid.New()

	_, err := testPool.Exec(t.Context(),
		`INSERT INTO avatars (id, user_id, file_name, mime_type, size_bytes, s3_key)
			VALUES ($1, 'user-1', 'avatar.png', 'image/png', 1024, 'avatars/user-1/original.png')`, id)
	require.NoError(t, err)

	stored, err := repo.GetByID(t.Context(), id)
	require.NoError(t, err)

	require.Equal(t, domain.UploadStatusUploading, stored.UploadStatus)
	require.Equal(t, domain.ProcessingStatusPending, stored.ProcessingStatus)
	require.Empty(t, stored.ThumbnailS3Keys)
	require.False(t, stored.CreatedAt.IsZero())
	require.False(t, stored.UpdatedAt.IsZero())
}

func TestGetByIDNotFound(t *testing.T) {
	repo := newRepo(t)

	_, err := repo.GetByID(t.Context(), uuid.New())
	require.ErrorIs(t, err, domain.ErrAvatarNotFound)
}

func TestGetByIDIgnoresDeleted(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SoftDelete(t.Context(), avatar.ID))

	_, err := repo.GetByID(t.Context(), avatar.ID)
	require.ErrorIs(t, err, domain.ErrAvatarNotFound)
}

func TestGetLatestByUser(t *testing.T) {
	repo := newRepo(t)

	older := createAvatar(t, repo, "user-1")
	shiftCreatedAt(t, older.ID, -time.Hour)

	newest := createAvatar(t, repo, "user-1")
	createAvatar(t, repo, "user-2")

	stored, err := repo.GetLatestByUser(t.Context(), "user-1")
	require.NoError(t, err)
	require.Equal(t, newest.ID, stored.ID)
}

func TestGetLatestByUserNotFound(t *testing.T) {
	repo := newRepo(t)

	_, err := repo.GetLatestByUser(t.Context(), "user-without-avatars")
	require.ErrorIs(t, err, domain.ErrAvatarNotFound)
}

func TestListByUserPaginates(t *testing.T) {
	repo := newRepo(t)

	first := createAvatar(t, repo, "user-1")
	shiftCreatedAt(t, first.ID, -2*time.Hour)

	second := createAvatar(t, repo, "user-1")
	shiftCreatedAt(t, second.ID, -time.Hour)

	third := createAvatar(t, repo, "user-1")
	createAvatar(t, repo, "user-2")

	page, err := repo.ListByUser(t.Context(), "user-1", 2, 0)
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Equal(t, third.ID, page[0].ID)
	require.Equal(t, second.ID, page[1].ID)

	page, err = repo.ListByUser(t.Context(), "user-1", 2, 2)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, first.ID, page[0].ID)
}

func TestListByUserSkipsDeleted(t *testing.T) {
	repo := newRepo(t)

	kept := createAvatar(t, repo, "user-1")
	removed := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SoftDelete(t.Context(), removed.ID))

	page, err := repo.ListByUser(t.Context(), "user-1", 10, 0)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, kept.ID, page[0].ID)
}

func TestListByUserEmpty(t *testing.T) {
	repo := newRepo(t)

	page, err := repo.ListByUser(t.Context(), "user-1", 10, 0)
	require.NoError(t, err)
	require.Empty(t, page)
}

func TestSetUploadStatus(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SetUploadStatus(t.Context(), avatar.ID, domain.UploadStatusFailed))

	stored, err := repo.GetByID(t.Context(), avatar.ID)
	require.NoError(t, err)
	require.Equal(t, domain.UploadStatusFailed, stored.UploadStatus)
}

func TestSetUploadStatusNotFound(t *testing.T) {
	repo := newRepo(t)

	err := repo.SetUploadStatus(t.Context(), uuid.New(), domain.UploadStatusUploaded)
	require.ErrorIs(t, err, domain.ErrAvatarNotFound)
}

func TestSetProcessingStatus(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SetProcessingStatus(t.Context(), avatar.ID, domain.ProcessingStatusProcessing))

	stored, err := repo.GetByID(t.Context(), avatar.ID)
	require.NoError(t, err)
	require.Equal(t, domain.ProcessingStatusProcessing, stored.ProcessingStatus)
	require.Equal(t, domain.PublicStatusProcessing, stored.PublicStatus())
}

func TestSetProcessingStatusOnDeleted(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SoftDelete(t.Context(), avatar.ID))

	err := repo.SetProcessingStatus(t.Context(), avatar.ID, domain.ProcessingStatusReady)
	require.ErrorIs(t, err, domain.ErrAvatarNotFound)
}

func TestSaveProcessingResult(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	thumbnails := map[string]string{
		"100x100": "avatars/user-1/100x100.png",
		"300x300": "avatars/user-1/300x300.png",
	}

	require.NoError(t, repo.SaveProcessingResult(t.Context(), avatar.ID, thumbnails, 640, 480))

	stored, err := repo.GetByID(t.Context(), avatar.ID)
	require.NoError(t, err)
	require.Equal(t, thumbnails, stored.ThumbnailS3Keys)
	require.Equal(t, 640, stored.Width)
	require.Equal(t, 480, stored.Height)
	require.Equal(t, domain.ProcessingStatusReady, stored.ProcessingStatus)
	require.Equal(t, domain.PublicStatusReady, stored.PublicStatus())

	key, err := stored.KeyForSize("300x300")
	require.NoError(t, err)
	require.Equal(t, thumbnails["300x300"], key)
}

func TestSaveProcessingResultWithoutThumbnails(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SaveProcessingResult(t.Context(), avatar.ID, nil, 100, 100))

	stored, err := repo.GetByID(t.Context(), avatar.ID)
	require.NoError(t, err)
	require.Empty(t, stored.ThumbnailS3Keys)
	require.Equal(t, domain.ProcessingStatusReady, stored.ProcessingStatus)
}

func TestSaveProcessingResultOnDeleted(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SoftDelete(t.Context(), avatar.ID))

	err := repo.SaveProcessingResult(t.Context(), avatar.ID, nil, 10, 10)
	require.ErrorIs(t, err, domain.ErrAvatarNotFound)
}

func TestSoftDeleteIsNotRepeatable(t *testing.T) {
	repo := newRepo(t)

	avatar := createAvatar(t, repo, "user-1")
	require.NoError(t, repo.SoftDelete(t.Context(), avatar.ID))
	require.ErrorIs(t, repo.SoftDelete(t.Context(), avatar.ID), domain.ErrAvatarNotFound)
}

func TestPing(t *testing.T) {
	repo := newRepo(t)

	require.NoError(t, repo.Ping(t.Context()))
}

func TestNewPool(t *testing.T) {
	pool, err := repository.NewPool(t.Context(), config.Postgres{
		DSN:         testDSN,
		MaxConns:    4,
		MinConns:    1,
		MaxConnLife: time.Minute,
	})
	require.NoError(t, err)
	defer pool.Close()

	require.NoError(t, pool.Ping(t.Context()))
}

func TestNewPoolRejectsInvalidDSN(t *testing.T) {
	_, err := repository.NewPool(t.Context(), config.Postgres{DSN: "://not-a-dsn", MaxConns: 1})
	require.Error(t, err)
}

func TestNewPoolFailsWhenUnreachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, err := repository.NewPool(ctx, config.Postgres{
		DSN:      "postgres://avatars:avatars@127.0.0.1:1/avatars?sslmode=disable&connect_timeout=1",
		MaxConns: 1,
		MinConns: 0,
	})
	require.Error(t, err)
}
