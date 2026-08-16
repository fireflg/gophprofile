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

func TestLoadDefaults(t *testing.T) {
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
}

func TestLoadFromJSONFile(t *testing.T) {
	path := writeConfig(t, `{
		"app": {"env": "production", "log_level": "warn"},
		"http": {"host": "127.0.0.1", "port": 9090, "read_timeout": "5s"},
		"postgres": {"dsn": "postgres://from-file/db"},
		"s3": {"bucket": "custom-bucket", "use_ssl": true},
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
	require.True(t, cfg.S3.UseSSL)
	require.Equal(t, []string{"kafka-1:9092", "kafka-2:9092"}, cfg.Kafka.Brokers)
	require.Equal(t, []config.Size{{Width: 50, Height: 50}}, cfg.Image.ThumbnailSizes)

	// То, чего нет в файле, остаётся значением по умолчанию.
	require.Equal(t, "avatars-worker", cfg.Kafka.GroupID)
}

func TestLoadFromEnv(t *testing.T) {
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
	path := writeConfig(t, `{"app": {"env": "production"}, "http": {"port": 9090}}`)

	t.Setenv("APP_ENV", "staging")
	t.Setenv("HTTP_PORT", "7070")

	cfg, err := config.LoadFrom(path)
	require.NoError(t, err)

	require.Equal(t, "staging", cfg.App.Env)
	require.Equal(t, 7070, cfg.HTTP.Port)
}

func TestLoadUsesConfigFileEnv(t *testing.T) {
	path := writeConfig(t, `{"s3": {"bucket": "from-config-file-env"}}`)

	t.Setenv("CONFIG_FILE", path)

	cfg, err := config.Load()
	require.NoError(t, err)

	require.Equal(t, "from-config-file-env", cfg.S3.Bucket)
}

func TestLoadMissingFileFails(t *testing.T) {
	_, err := config.LoadFrom(filepath.Join(t.TempDir(), "нет-такого.json"))
	require.Error(t, err)
}

func TestLoadInvalidJSONFails(t *testing.T) {
	_, err := config.LoadFrom(writeConfig(t, `{"app": `))
	require.Error(t, err)
}

func TestValidationErrorsFromEnv(t *testing.T) {
	tests := map[string]struct {
		env  map[string]string
		want string
	}{
		"нулевой размер файла": {env: map[string]string{"MAX_FILE_SIZE": "0"}, want: "max_file_size"},
		"нулевой порт":         {env: map[string]string{"HTTP_PORT": "0"}, want: "http.port"},
		"кривой размер превью": {env: map[string]string{"THUMBNAIL_SIZES": "100"}, want: "thumbnail size"},
		"нечисловая ширина":    {env: map[string]string{"THUMBNAIL_SIZES": "axb"}, want: "thumbnail width"},
		"нулевая высота":       {env: map[string]string{"THUMBNAIL_SIZES": "100x0"}, want: "thumbnail height"},
		"пустой список превью": {env: map[string]string{"THUMBNAIL_SIZES": " "}, want: "thumbnail_sizes"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			_, err := config.LoadFrom("")
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// Пустая переменная окружения для viper равна незаданной, поэтому обязательные
// поля можно обнулить только явным пустым значением в файле.
func TestValidationErrorsFromFile(t *testing.T) {
	tests := map[string]struct {
		content string
		want    string
	}{
		"пустой dsn":       {content: `{"postgres": {"dsn": ""}}`, want: "postgres.dsn"},
		"пустой bucket":    {content: `{"s3": {"bucket": ""}}`, want: "s3.bucket"},
		"пустые mime-типы": {content: `{"image": {"allowed_mime_types": []}}`, want: "allowed_mime_types"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := config.LoadFrom(writeConfig(t, tc.content))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestSizeString(t *testing.T) {
	require.Equal(t, "100x100", config.Size{Width: 100, Height: 100}.String())
	require.Equal(t, "1920x1080", config.Size{Width: 1920, Height: 1080}.String())
}
