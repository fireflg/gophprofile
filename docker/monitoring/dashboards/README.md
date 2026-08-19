# Дашборды Grafana

Положенный сюда JSON подхватывается провижинингом (`../grafana-dashboards.yml`),
импортировать руками ничего не нужно. Подкаталоги становятся папками в Grafana.

Панели ссылаются на датасорс по `uid: prometheus` - он зафиксирован в
`../grafana-datasource.yml`.

| Файл | Что показывает |
|---|---|
| `avatars-service.json` | загрузки и нарезка, HTTP, рантайм Go, PostgreSQL и S3 |
