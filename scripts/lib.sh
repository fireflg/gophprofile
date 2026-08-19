#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$ROOT/docker/docker-compose.yml}"
KAFKA_SERVICE="${KAFKA_SERVICE:-kafka}"
KAFKA_BOOTSTRAP="${KAFKA_BOOTSTRAP:-localhost:9092}"
KAFKA_TOPIC="${KAFKA_TOPIC:-avatars.events}"
KAFKA_GROUP_ID="${KAFKA_GROUP_ID:-avatars-worker}"

kafka_exec() {
  docker compose -f "$COMPOSE_FILE" exec -T "$KAFKA_SERVICE" "$@"
}
