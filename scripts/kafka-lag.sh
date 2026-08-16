#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

GROUP="${1:-$KAFKA_GROUP_ID}"

kafka_exec kafka-consumer-groups --bootstrap-server "$KAFKA_BOOTSTRAP" --describe --group "$GROUP"
