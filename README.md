# avatars-service

Сервис загрузки и выдачи аватарок: HTTP API на chi, метаданные в PostgreSQL,
файлы в S3, асинхронная нарезка миниатюр через воркер и брокер.


## Конфигурация

Источники по возрастанию приоритета: значения по умолчанию → JSON-файл → переменные окружения.
Путь к файлу задаётся переменной `CONFIG_FILE`; без неё используются только окружение и умолчания.

Обязательные параметры

| Параметр | Переменная окружения |
|---|---|
| `postgres.dsn` | `POSTGRES_DSN` |
| `s3.access_key` | `S3_ACCESS_KEY` |
| `s3.secret_key` | `S3_SECRET_KEY` |

```bash
cp config.example.json config.json   # подставьте свои реквизиты доступа
CONFIG_FILE=config.json make run-server

# либо только через окружение
POSTGRES_DSN=postgres://... S3_ACCESS_KEY=... S3_SECRET_KEY=... make run-server
```

Соответствие ключей и переменных окружения задано в `internal/config/config.go`
(например, `http.port` → `HTTP_PORT`, `image.thumbnail_sizes` → `THUMBNAIL_SIZES`).
Списки в переменных окружения перечисляются через запятую.

События публикуются в Kafka в фоне: ответ пользователю не ждёт подтверждения брокера.
Число одновременных отправок ограничено (`maxPublishInFlight` в
`internal/services/avatar_service.go`); при исчерпании лимита событие отправляется на
месте. Предел на одну отправку задаётся `kafka.write_timeout` (`KAFKA_WRITE_TIMEOUT`,
по умолчанию `5s`).

## Запуск

```bash
cp .env.example .env
docker compose -f docker/docker-compose.yml up -d --build   # весь стенд целиком
make run-server
make run-worker
make test
```

Стенд разложен по слоям, каждый поднимается и отдельно:

```
docker/infrastructure/   postgres, миграции, minio, kafka
docker/application/      server, worker, Dockerfile
docker/monitoring/       коллектор, jaeger, opensearch, prometheus, grafana, экспортёры
```

```bash
make up-infra        # только хранилища и брокер: хватает для make run-server
make up-monitoring   # только наблюдаемость
```

## Наблюдаемость

Три сигнала. Трейсы и логи процессы отправляют по OTLP в коллектор, метрики
коллектор не трогает: у них модель pull, Prometheus скребёт сервер и воркер
напрямую по служебному порту.

| UI | Адрес | Что там |
|---|---|---|
| Jaeger | `localhost:16686` | трейсы запросов и связанные с ними трейсы обработки событий |
| OpenSearch Dashboards | `localhost:5601` | логи; индекс `otel-logs`, index pattern `otel-logs*` |
| Prometheus | `localhost:9090` | цели скрейпа и запросы |
| Grafana | `localhost:3000` | графики; датасорс и дашборды подключены провижинингом |

Метрики самих процессов доступны на `localhost:9101/metrics` (сервер) и
`localhost:9102/metrics` (воркер): порт `9090` на хосте занимает Prometheus.

```bash
curl -s localhost:9101/metrics | grep avatars_
```

| Метрика | Лейблы |
|---|---|
| `avatars_uploads_total`, `avatars_upload_duration_seconds` | `status` |
| `avatars_thumbnails_total`, `avatars_processing_duration_seconds` | `status` |
| `avatars_http_requests_total`, `avatars_http_errors_total` | `method`, `route`, `status` |
| `avatars_http_request_duration_seconds` | `method`, `route` |
| `avatars_storage_bytes` | нет; отдаёт только сервер |

`route` — шаблон маршрута (`/api/v1/avatars/{id}`), а не путь: иначе
идентификаторы в URL расплодили бы ряды по числу аватарок. По той же причине
`user_id` в лейблах нет вовсе — разрез по пользователю живёт в трейсах и логах.

`avatars_storage_bytes` считается запросом `SUM(size_bytes)` по всей таблице,
поэтому значение кэшируется на минуту: скрейпов может быть много (несколько
реплик Prometheus, ручной `curl`), а нагрузка на БД от них не зависит.
Кэшируются и неудачные попытки — иначе сбой БД вернул бы запрос на каждый
скрейп ровно тогда, когда ей тяжелее всего.

Кроме приложения Prometheus скребёт экспортёры инфраструктуры: `postgres-exporter`
(`:9187`), `kafka-exporter` (`:9308`) и сам MinIO — отдельного экспортёра у него
нет, метрики он отдаёт на `/minio/v2/metrics/cluster`.

Преднастроенные дашборды лежат в `docker/monitoring/dashboards/`: Grafana
подхватывает оттуда JSON провижинингом, импортировать руками ничего не нужно.
Готовый `avatars-service.json` собирает бизнес-метрики (загрузки, нарезка,
объём хранилища), HTTP и рантайм Go; переменная «Сервис» переключает разрез
между сервером и воркером.

### Поиск логов по пользователю

Каждая запись, сделанная в рамках запроса, несёт `trace_id`, `span_id`,
`request_id` и `user_id`. Идентификатор пользователя прослойка `middleware.UserID`
кладёт в контекст из заголовка `X-User-ID`, а хендлер логгера подставляет его
полем в каждую запись с контекстом — не только в строку доступа. У чтений
заголовка нет, поэтому строка доступа добирает идентификатор из параметра
маршрута
(`/api/v1/users/{user_id}/...`). В воркер `user_id` приезжает в теле события, так
что логи нарезки миниатюр находятся тем же запросом, что и логи загрузки.

В OpenSearch Dashboards (Discover, index pattern `otel-logs*`) фильтр выглядит так:

```
attributes.user_id: "user-1"
```

Префикс зависит от того, как экспортёр коллектора разложил атрибуты записи:
точное имя поля видно в развёрнутой записи в Discover, и если корень плоский,
фильтр пишется как `user_id: "user-1"`. Дальше из той же записи берётся
`trace_id` и вставляется в поиск Jaeger — открывается весь запрос целиком,
вместе с походами в S3.

Выключается всё независимо: `OTEL_ENABLED=false` гасит экспорт трейсов и логов,
`METRICS_ENABLED=false` — порт метрик. Сервис работает в обоих случаях.

## Скрипты для кафки

Скрипты работают через `docker compose exec kafka`, поэтому брокер должен быть запущен.
Топик, группа и путь к compose-файлу переопределяются переменными окружения
(`KAFKA_TOPIC`, `KAFKA_GROUP_ID`, `COMPOSE_FILE`, `KAFKA_BOOTSTRAP`).

```bash
scripts/kafka-tail.sh            # все сообщения топика с ключами и заголовками
scripts/kafka-tail.sh -n 5 -j    # последние 5 сообщений, только тело, через jq
scripts/kafka-tail.sh -f         # следить за новыми
scripts/kafka-topic-info.sh      # список топиков, партиции, границы смещений
scripts/kafka-lag.sh             # отставание группы avatars-worker
```

## API

| Метод | Путь | Ответ |
|---|---|---|
| POST | `/api/v1/avatars` | 201 `{id, user_id, url, status, created_at}` |
| GET | `/api/v1/avatars/{id}?size=100x100&format=jpeg` | 200 бинарные данные |
| GET | `/api/v1/avatars/{id}/metadata` | 200 метаданные с миниатюрами |
| DELETE | `/api/v1/avatars/{id}` | 204 |
| GET | `/api/v1/users/{user_id}/avatar` | 200 последняя аватарка |
| DELETE | `/api/v1/users/{user_id}/avatar` | 204 |
| GET | `/api/v1/users/{user_id}/avatars` | 200 список |
| GET | `/health` | 200/503 статусы компонентов |
| GET | `/` | 200 страница загрузки и галерея (`web/static/index.html`) |

Изменяющие запросы требуют заголовок `X-User-ID`.

Коды ошибок: 400 (формат/идентификатор), 401 (нет `X-User-ID`), 403 (чужая аватарка),
404 (нет аватарки), 409 (миниатюра ещё обрабатывается), 413 (файл больше лимита).

