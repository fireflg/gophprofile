# avatars-service

Сервис загрузки и выдачи аватарок: HTTP API на chi, метаданные в PostgreSQL,
файлы в S3, асинхронная нарезка миниатюр через воркер и брокер.


## Конфигурация

Источники по возрастанию приоритета: значения по умолчанию → JSON-файл → переменные окружения.
Путь к файлу задаётся переменной `CONFIG_FILE`; без неё используются только окружение и умолчания.

```bash
cp config.example.json config.json
CONFIG_FILE=config.json make run-server

# либо только через окружение
HTTP_PORT=9090 POSTGRES_DSN=postgres://... make run-server
```

Соответствие ключей и переменных окружения задано в `internal/config/config.go`
(например, `http.port` → `HTTP_PORT`, `image.thumbnail_sizes` → `THUMBNAIL_SIZES`).
Списки в переменных окружения перечисляются через запятую.

## Запуск

```bash
cp .env.example .env
docker compose -f docker/docker-compose.yml up -d --build   # postgres, minio, kafka, миграции
make run-server
make run-worker
make test
```

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

