#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

echo "== topics =="
kafka_exec kafka-topics --bootstrap-server "$KAFKA_BOOTSTRAP" --list

echo
echo "== $KAFKA_TOPIC =="
kafka_exec kafka-topics --bootstrap-server "$KAFKA_BOOTSTRAP" --describe --topic "$KAFKA_TOPIC"

echo
echo "== offsets =="
kafka_exec kafka-run-class kafka.tools.GetOffsetShell \
  --bootstrap-server "$KAFKA_BOOTSTRAP" --topic "$KAFKA_TOPIC"
