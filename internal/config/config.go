// Package config загружает конфигурацию сервиса через viper.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config - конфигурация сервиса.
type Config struct {
	App      App      `mapstructure:"app"`
	HTTP     HTTP     `mapstructure:"http"`
	Postgres Postgres `mapstructure:"postgres"`
	S3       S3       `mapstructure:"s3"`
	Kafka    Kafka    `mapstructure:"kafka"`
	Image    Image    `mapstructure:"image"`
	OTel     OTel     `mapstructure:"otel"`
	Metrics  Metrics  `mapstructure:"metrics"`
}

type App struct {
	Env      string `mapstructure:"env"`
	LogLevel string `mapstructure:"log_level"`
}

type HTTP struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
}

// Addr - адрес прослушивания HTTP-сервера.
func (h HTTP) Addr() string {
	return fmt.Sprintf("%s:%d", h.Host, h.Port)
}

type Postgres struct {
	DSN         string        `mapstructure:"dsn"`
	MaxConns    int32         `mapstructure:"max_conns"`
	MinConns    int32         `mapstructure:"min_conns"`
	MaxConnLife time.Duration `mapstructure:"max_conn_lifetime"`
}

type S3 struct {
	Endpoint      string `mapstructure:"endpoint"`
	AccessKey     string `mapstructure:"access_key"`
	SecretKey     string `mapstructure:"secret_key"`
	Bucket        string `mapstructure:"bucket"`
	Region        string `mapstructure:"region"`
	UseSSL        bool   `mapstructure:"use_ssl"`
	PublicBaseURL string `mapstructure:"public_base_url"`
}

type Kafka struct {
	Brokers      []string      `mapstructure:"brokers"`
	Topic        string        `mapstructure:"topic"`
	GroupID      string        `mapstructure:"group_id"`
	ClientID     string        `mapstructure:"client_id"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// Image - ограничения загрузки и набор миниатюр.
type Image struct {
	MaxFileSize      int64    `mapstructure:"max_file_size"`
	AllowedMimeTypes []string `mapstructure:"allowed_mime_types"`
	// RawThumbnailSizes задаётся как ["100x100", "300x300"], разбирается в ThumbnailSizes.
	RawThumbnailSizes []string `mapstructure:"thumbnail_sizes"`
	ThumbnailSizes    []Size   `mapstructure:"-"`
}

// Size - размер миниатюры.
type Size struct {
	Width  int
	Height int
}

// String возвращает каноническое имя размера, например "100x100".
func (s Size) String() string {
	return fmt.Sprintf("%dx%d", s.Width, s.Height)
}

// OTel - экспорт трейсов и логов в OTLP-коллектор.
type OTel struct {
	Enabled     bool    `mapstructure:"enabled"`
	Endpoint    string  `mapstructure:"endpoint"`
	ServiceName string  `mapstructure:"service_name"`
	Insecure    bool    `mapstructure:"insecure"`
	SampleRatio float64 `mapstructure:"sample_ratio"`
}

// Active - признак того, что экспорт настроен: без адреса коллектора
// включённый флаг ничего не значит.
func (o OTel) Active() bool {
	return o.Enabled && o.Endpoint != ""
}

// Metrics - отдельный HTTP-endpoint с метриками в формате Prometheus.
//
// От OTel не зависит: модель pull, коллектор для метрик не нужен.
type Metrics struct {
	Enabled bool   `mapstructure:"enabled"`
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

// Addr - адрес прослушивания сервера метрик.
func (m Metrics) Addr() string {
	return fmt.Sprintf("%s:%d", m.Host, m.Port)
}

// envBindings - соответствие ключей конфигурации переменным окружения.
//
//nolint:gosec // G101.
var envBindings = map[string]string{
	"app.env":       "APP_ENV",
	"app.log_level": "LOG_LEVEL",

	"http.host":             "HTTP_HOST",
	"http.port":             "HTTP_PORT",
	"http.read_timeout":     "HTTP_READ_TIMEOUT",
	"http.write_timeout":    "HTTP_WRITE_TIMEOUT",
	"http.shutdown_timeout": "HTTP_SHUTDOWN_TIMEOUT",

	"postgres.dsn":               "POSTGRES_DSN",
	"postgres.max_conns":         "POSTGRES_MAX_CONNS",
	"postgres.min_conns":         "POSTGRES_MIN_CONNS",
	"postgres.max_conn_lifetime": "POSTGRES_MAX_CONN_LIFETIME",

	"s3.endpoint":        "S3_ENDPOINT",
	"s3.access_key":      "S3_ACCESS_KEY",
	"s3.secret_key":      "S3_SECRET_KEY",
	"s3.bucket":          "S3_BUCKET",
	"s3.region":          "S3_REGION",
	"s3.use_ssl":         "S3_USE_SSL",
	"s3.public_base_url": "S3_PUBLIC_BASE_URL",

	"kafka.brokers":       "KAFKA_BROKERS",
	"kafka.topic":         "KAFKA_TOPIC",
	"kafka.group_id":      "KAFKA_GROUP_ID",
	"kafka.client_id":     "KAFKA_CLIENT_ID",
	"kafka.write_timeout": "KAFKA_WRITE_TIMEOUT",

	"image.max_file_size":      "MAX_FILE_SIZE",
	"image.allowed_mime_types": "ALLOWED_MIME_TYPES",
	"image.thumbnail_sizes":    "THUMBNAIL_SIZES",

	"otel.enabled":      "OTEL_ENABLED",
	"otel.endpoint":     "OTEL_EXPORTER_OTLP_ENDPOINT",
	"otel.service_name": "OTEL_SERVICE_NAME",
	"otel.insecure":     "OTEL_EXPORTER_OTLP_INSECURE",
	"otel.sample_ratio": "OTEL_TRACES_SAMPLER_ARG",

	"metrics.enabled": "METRICS_ENABLED",
	"metrics.host":    "METRICS_HOST",
	"metrics.port":    "METRICS_PORT",
	"metrics.path":    "METRICS_PATH",
}

var requiredKeys = []string{
	"postgres.dsn",
	"s3.access_key",
	"s3.secret_key",
}

// Load читает конфигурацию.
func Load() (*Config, error) {
	return LoadFrom(os.Getenv("CONFIG_FILE"))
}

// LoadFrom читает конфигурацию, явно указывая путь к JSON-файлу (может быть пустым).
func LoadFrom(configFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("json")

	setDefaults(v)

	for key, env := range envBindings {
		if err := v.BindEnv(key, env); err != nil {
			return nil, fmt.Errorf("config: bind env %s: %w", env, err)
		}
	}

	if configFile != "" {
		v.SetConfigFile(configFile)

		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: read %s: %w", configFile, err)
		}
	}

	if err := checkRequired(v); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	sizes, err := parseSizes(cfg.Image.RawThumbnailSizes)
	if err != nil {
		return nil, err
	}
	cfg.Image.ThumbnailSizes = sizes

	return &cfg, nil
}

func checkRequired(v *viper.Viper) error {
	for _, key := range requiredKeys {
		if strings.TrimSpace(v.GetString(key)) == "" {
			return fmt.Errorf("config: %s is required, set it in the config file or via %s", key, envBindings[key])
		}
	}

	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("app.env", "development")
	v.SetDefault("app.log_level", "info")

	v.SetDefault("http.host", "0.0.0.0")
	v.SetDefault("http.port", 8080)
	v.SetDefault("http.read_timeout", "30s")
	v.SetDefault("http.write_timeout", "60s")
	v.SetDefault("http.shutdown_timeout", "15s")

	v.SetDefault("postgres.max_conns", 10)
	v.SetDefault("postgres.min_conns", 2)
	v.SetDefault("postgres.max_conn_lifetime", "1h")

	v.SetDefault("s3.endpoint", "localhost:9000")
	v.SetDefault("s3.bucket", "avatars")
	v.SetDefault("s3.region", "us-east-1")
	v.SetDefault("s3.use_ssl", false)
	v.SetDefault("s3.public_base_url", "")

	v.SetDefault("kafka.brokers", []string{"localhost:9092"})
	v.SetDefault("kafka.topic", "avatars.events")
	v.SetDefault("kafka.group_id", "avatars-worker")
	v.SetDefault("kafka.client_id", "avatars-service")
	v.SetDefault("kafka.write_timeout", "5s")

	v.SetDefault("image.max_file_size", 10*1024*1024)
	v.SetDefault("image.allowed_mime_types", []string{"image/jpeg", "image/png", "image/webp"})
	v.SetDefault("image.thumbnail_sizes", []string{"100x100", "300x300"})

	v.SetDefault("otel.enabled", false)
	v.SetDefault("otel.endpoint", "")
	v.SetDefault("otel.service_name", "avatars-service")
	v.SetDefault("otel.insecure", true)
	v.SetDefault("otel.sample_ratio", 1.0)

	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.host", "0.0.0.0")
	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")
}

func parseSizes(raw []string) ([]Size, error) {
	sizes := make([]Size, 0, len(raw))

	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		dims := strings.SplitN(item, "x", 2)
		if len(dims) != 2 {
			return nil, fmt.Errorf("config: invalid thumbnail size %q, expected WIDTHxHEIGHT", item)
		}

		width, err := strconv.Atoi(strings.TrimSpace(dims[0]))
		if err != nil || width <= 0 {
			return nil, fmt.Errorf("config: invalid thumbnail width in %q", item)
		}

		height, err := strconv.Atoi(strings.TrimSpace(dims[1]))
		if err != nil || height <= 0 {
			return nil, fmt.Errorf("config: invalid thumbnail height in %q", item)
		}

		sizes = append(sizes, Size{Width: width, Height: height})
	}

	return sizes, nil
}
