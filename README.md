# avatars-service

Сервис загрузки и выдачи аватарок: HTTP API на chi, метаданные в PostgreSQL,
файлы в S3, асинхронная нарезка миниатюр через воркер и брокер.


Инфраструктура (PostgreSQL, S3, Kafka, мониторинг, Vault) в кластере чартом не
разворачивается: её адреса задаются в values.


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

## Деплой в Kubernetes

Чарт лежит в `deploy/helm/avatars-service`, окружения различаются только values:
`values.yaml` (база, локальный kind), `values-staging.yaml`, `values-production.yaml`.
Один образ на оба процесса, бинарь выбирается аргументом сборки:

```bash
docker build -f docker/application/Dockerfile --build-arg APP=server -t registry.example.com/avatars-service:release .
docker push registry.example.com/avatars-service:release
```

Воркеру отдельный образ не нужен: `cmd/worker` собирается тем же Dockerfile
(`--build-arg APP=worker`), если вы держите бинарники в разных образах — задайте
свой `image.repository` в values воркера.

```bash
make helm-sync                        # копирует migrations/*.sql внутрь чарта
make helm-lint
helm upgrade --install avatars-service deploy/helm/avatars-service \
  -n avatars --create-namespace \
  -f deploy/helm/avatars-service/values-staging.yaml \
  --set image.repository=registry.example.com/avatars-service
```

`make helm-sync` обязателен после правки миграций: Helm читает файлы только внутри
каталога чарта, поэтому в `files/migrations/` лежит копия `migrations/`.

Проверка после установки:

```bash
kubectl -n avatars get po,svc,hpa,netpol,servicemonitor
kubectl -n avatars logs deploy/avatars-service-server -c migrations
kubectl -n avatars port-forward svc/avatars-service 8080:8080   # UI и API
kubectl -n avatars port-forward svc/avatars-service 9090:9090   # /metrics
```

### Что внутри чарта

| Ресурс | Замечания |
|---|---|
| `Deployment` server | порты 8080 и 9090, init-контейнеры vault-agent и migrate |
| `Deployment` worker | только 9090; миграций нет — иначе два раннера гоняются за `schema_migrations` |
| `Service` × 2 | обычный для сервера, headless для воркера: `ServiceMonitor` селектит именно сервисы |
| `ServiceMonitor` | порт `metrics`, включается `serviceMonitor.enabled` (нужны CRD prometheus-operator) |
| `HPA` × 2 | `autoscaling/v2`, CPU и память по utilization; требует заданных `resources.requests` |
| `ConfigMap`, `Secret` | имена ключей совпадают с `envBindings` из `internal/config/config.go` |
| `ServiceAccount`, `Role`, `RoleBinding` | права только на `get`/`watch` своих ConfigMap и Secret |
| `NetworkPolicy` × 2 | default-deny плюс точечные разрешения: DNS, ingress-контроллер, мониторинг, внешняя инфраструктура; выключаются целиком (`networkPolicy.enabled`) или по частям |
| `PodDisruptionBudget` | включается в проде |
| `Ingress` | опциональный, по умолчанию выключен |

### Секреты и миграции

Оба этапа сделаны init-контейнерами, а не Helm-хуками: хук выполняется отдельным
подом вне жизненного цикла пода приложения, а init-контейнер гарантированно
отрабатывает перед каждым стартом контейнера и в том же сетевом и security-контексте.

При `vault.enabled=true` первым идёт `vault-agent`: аутентифицируется в Vault
по токену ServiceAccount и рендерит `/vault/secrets/config.json` (`postgres.dsn`,
`s3.access_key`, `s3.secret_key`) в `emptyDir` с `medium: Memory`. Приложению
ставится `CONFIG_FILE=/vault/secrets/config.json` — viper уже умеет читать JSON
по этому пути, менять код не пришлось. Вторым идёт `migrate` из образа
`migrate/migrate`, SQL монтируется из ConfigMap.

При `vault.enabled=false` (умолчание для kind/minikube) те же значения приходят
из `Secret` через `envFrom`; env в viper приоритетнее файла, так что путь один и тот же.

### Безопасность подов

`runAsNonRoot: true`, `runAsUser: 10001` (совпадает с пользователем `app` из
`docker/application/Dockerfile`), `readOnlyRootFilesystem: true` c `emptyDir` на
`/tmp`, `allowPrivilegeEscalation: false`, `capabilities.drop: [ALL]`,
`seccompProfile: RuntimeDefault`. Токен ServiceAccount монтируется только когда
включён Vault.

PodSecurityPolicy удалён в Kubernetes 1.25, поэтому ограничения уровня пода
задаются Pod Security Admission — лейблами на неймспейсе:

```bash
kubectl label ns avatars \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/warn=restricted \
  pod-security.kubernetes.io/audit=restricted
```

Если неймспейс создаёт сам чарт, те же лейблы навешивает `podSecurity.labelNamespace=true`.

### Пробы

| Процесс | readiness | liveness |
|---|---|---|
| server | `GET /health:8080` — проверяет postgres, S3 и брокер, под уходит из эндпоинтов, пока они недоступны | `GET /metrics:9090` — без внешних зависимостей, рестарт только если процесс завис |
| worker | `GET /metrics:9090` | `GET /metrics:9090` |

Разделение намеренное: если повесить liveness на `/health`, кратковременная
недоступность БД перезапустит все поды разом вместо того, чтобы просто снять
их с балансировки.

## Наблюдаемость

Три сигнала. Трейсы и логи процессы отправляют по OTLP в коллектор, метрики
коллектор не трогает: у них модель pull, Prometheus скерйпит сервер и воркер
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

