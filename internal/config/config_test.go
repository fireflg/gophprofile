package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fireflg/gophprofile/internal/config"
)

// writeConfig кладёт JSON-конфиг во временный каталог теста.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func setSecrets(t *testing.T) {
	t.Helper()

	t.Setenv("POSTGRES_DSN", "postgres://test/db")
	t.Setenv("S3_ACCESS_KEY", "test-access-key")
	t.Setenv("S3_SECRET_KEY", "test-secret-key")
}

func TestLoadDefaults(t *testing.T) {
	setSecrets(t)

	cfg, err := config.LoadFrom("")
	require.NoError(t, err)

	require.Equal(t, "development", cfg.App.Env)
	require.Equal(t, "info", cfg.App.LogLevel)
	require.Equal(t, "0.0.0.0:8080", cfg.HTTP.Addr())
	require.Equal(t, 30*time.Second, cfg.HTTP.ReadTimeout)
	require.Equal(t, int64(10*1024*1024), cfg.Image.MaxFileSize)
	require.Equal(t, []string{"image/jpeg", "image/png", "image/webp"}, cfg.Image.AllowedMimeTypes)
	require.Equal(t, []config.Size{{Width: 100, Height: 100}, {Width: 300, Height: 300}}, cfg.Image.ThumbnailSizes)
	require.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
	require.Equal(t, 5*time.Second, cfg.Kafka.WriteTimeout)
}

func TestLoadFromJSONFile(t *testing.T) {
	path := writeConfig(t, `{
		"app": {"env": "production", "log_level": "warn"},
		"http": {"host": "127.0.0.1", "port": 9090, "read_timeout": "5s"},
		"postgres": {"dsn": "postgres://from-file/db"},
		"s3": {"bucket": "custom-bucket", "use_ssl": true, "access_key": "file-key", "secret_key": "file-secret"},
		"kafka": {"brokers": ["kafka-1:9092", "kafka-2:9092"], "topic": "custom.events"},
		"image": {"max_file_size": 2048, "thumbnail_sizes": ["50x50"]}
	}`)

	cfg, err := config.LoadFrom(path)
	require.NoError(t, err)

	require.Equal(t, "production", cfg.App.Env)
	require.Equal(t, "127.0.0.1:9090", cfg.HTTP.Addr())
	require.Equal(t, 5*time.Second, cfg.HTTP.ReadTimeout)
	require.Equal(t, "postgres://from-file/db", cfg.Postgres.DSN)
	require.Equal(t, "custom-bucket", cfg.S3.Bucket)
	require.Equal(t, "file-key", cfg.S3.AccessKey)
	require.True(t, cfg.S3.UseSSL)
	require.Equal(t, []string{"kafka-1:9092", "kafka-2:9092"}, cfg.Kafka.Brokers)
	require.Equal(t, []config.Size{{Width: 50, Height: 50}}, cfg.Image.ThumbnailSizes)

	// То, чего нет в файле, остаётся значением по умолчанию.
	require.Equal(t, "avatars-worker", cfg.Kafka.GroupID)
}

func TestLoadFromEnv(t *testing.T) {
	setSecrets(t)

	t.Setenv("APP_ENV", "staging")
	t.Setenv("HTTP_PORT", "7070")
	t.Setenv("HTTP_WRITE_TIMEOUT", "45s")
	t.Setenv("POSTGRES_DSN", "postgres://env/db")
	t.Setenv("POSTGRES_MAX_CONNS", "42")
	t.Setenv("S3_BUCKET", "env-bucket")
	t.Setenv("S3_USE_SSL", "true")
	t.Setenv("KAFKA_BROKERS", "broker-1:9092,broker-2:9092")
	t.Setenv("MAX_FILE_SIZE", "4096")
	t.Setenv("ALLOWED_MIME_TYPES", "image/png,image/webp")
	t.Setenv("THUMBNAIL_SIZES", "64x64,128x128")

	cfg, err := config.LoadFrom("")
	require.NoError(t, err)

	require.Equal(t, "staging", cfg.App.Env)
	require.Equal(t, 7070, cfg.HTTP.Port)
	require.Equal(t, 45*time.Second, cfg.HTTP.WriteTimeout)
	require.Equal(t, "postgres://env/db", cfg.Postgres.DSN)
	require.Equal(t, int32(42), cfg.Postgres.MaxConns)
	require.Equal(t, "env-bucket", cfg.S3.Bucket)
	require.True(t, cfg.S3.UseSSL)
	require.Equal(t, []string{"broker-1:9092", "broker-2:9092"}, cfg.Kafka.Brokers)
	require.Equal(t, int64(4096), cfg.Image.MaxFileSize)
	require.Equal(t, []string{"image/png", "image/webp"}, cfg.Image.AllowedMimeTypes)
	require.Equal(t, []config.Size{{Width: 64, Height: 64}, {Width: 128, Height: 128}}, cfg.Image.ThumbnailSizes)
}

// Окружение перекрывает файл: так контейнер настраивается без пересборки образа.
func TestEnvOverridesFile(t *testing.T) {
	setSecrets(t)

	path := writeConfig(t, `{"app": {"env": "production"}, "http": {"port": 9090}}`)

	t.Setenv("APP_ENV", "staging")
	t.Setenv("HTTP_PORT", "7070")

	cfg, err := config.LoadFrom(path)
	require.NoError(t, err)

	require.Equal(t, "staging", cfg.App.Env)
	require.Equal(t, 7070, cfg.HTTP.Port)
}

func TestLoadUsesConfigFileEnv(t *testing.T) {
	setSecrets(t)

	path := writeConfig(t, `{"s3": {"bucket": "from-config-file-env"}}`)

	t.Setenv("CONFIG_FILE", path)

	cfg, err := config.Load()
	require.NoError(t, err)

	require.Equal(t, "from-config-file-env", cfg.S3.Bucket)
}

func TestLoadRequiresSecrets(t *testing.T) {
	tests := map[string]struct {
		set  map[string]string
		want string
	}{
		"нет dsn": {
			set:  map[string]string{"S3_ACCESS_KEY": "key", "S3_SECRET_KEY": "secret"},
			want: "postgres.dsn is required",
		},
		"нет ключа s3": {
			set:  map[string]string{"POSTGRES_DSN": "postgres://test/db", "S3_SECRET_KEY": "secret"},
			want: "s3.access_key is required",
		},
		"нет секрета s3": {
			set:  map[string]string{"POSTGRES_DSN": "postgres://test/db", "S3_ACCESS_KEY": "key"},
			want: "s3.secret_key is required",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			for key, value := range tc.set {
				t.Setenv(key, value)
			}

			_, err := config.LoadFrom("")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLoadRejectsBlankSecretInFile(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://test/db")
	t.Setenv("S3_ACCESS_KEY", "test-access-key")

	_, err := config.LoadFrom(writeConfig(t, `{"s3": {"secret_key": "   "}}`))
	require.ErrorContains(t, err, "s3.secret_key is required")
}

func TestLoadMissingFileFails(t *testing.T) {
	_, err := config.LoadFrom(filepath.Join(t.TempDir(), "нет-такого.json"))
	require.Error(t, err)
}

func TestLoadInvalidJSONFails(t *testing.T) {
	_, err := config.LoadFrom(writeConfig(t, `{"app": `))
	require.Error(t, err)
}

func TestThumbnailSizesParseErrors(t *testing.T) {
	tests := map[string]struct {
		value string
		want  string
	}{
		"кривой размер превью": {value: "100", want: "thumbnail size"},
		"нечисловая ширина":    {value: "axb", want: "thumbnail width"},
		"нулевая высота":       {value: "100x0", want: "thumbnail height"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			setSecrets(t)
			t.Setenv("THUMBNAIL_SIZES", tc.value)

			_, err := config.LoadFrom("")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestLoadDoesNotCheckBusinessRules(t *testing.T) {
	setSecrets(t)

	t.Setenv("MAX_FILE_SIZE", "0")
	t.Setenv("HTTP_PORT", "0")
	t.Setenv("THUMBNAIL_SIZES", " ")

	path := writeConfig(t, `{"s3": {"bucket": ""}, "image": {"allowed_mime_types": []}}`)

	cfg, err := config.LoadFrom(path)
	require.NoError(t, err)

	require.Zero(t, cfg.Image.MaxFileSize)
	require.Zero(t, cfg.HTTP.Port)
	require.Empty(t, cfg.Image.ThumbnailSizes)
	require.Empty(t, cfg.S3.Bucket)
	require.Empty(t, cfg.Image.AllowedMimeTypes)
}

func TestSizeString(t *testing.T) {
	require.Equal(t, "100x100", config.Size{Width: 100, Height: 100}.String())
	require.Equal(t, "1920x1080", config.Size{Width: 1920, Height: 1080}.String())
}
