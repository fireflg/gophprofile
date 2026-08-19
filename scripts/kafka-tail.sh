#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

FROM_BEGINNING=1
MAX_MESSAGES=""
TIMEOUT_MS="${KAFKA_TIMEOUT_MS:-5000}"
PRETTY=0

usage() {
  cat <<EOF
Usage: $(basename "$0") [-f] [-n N] [-j] [-t topic]

  -f  follow: читать только новые сообщения и не выходить
  -n  прочитать не больше N сообщений
  -j  печатать только значение через jq
  -t  топик (по умолчанию $KAFKA_TOPIC)
EOF
}

while getopts ":fn:jt:h" opt; do
  case "$opt" in
    f) FROM_BEGINNING=0 ;;
    n) MAX_MESSAGES="$OPTARG" ;;
    j) PRETTY=1 ;;
    t) KAFKA_TOPIC="$OPTARG" ;;
    h) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
done

args=(kafka-console-consumer --bootstrap-server "$KAFKA_BOOTSTRAP" --topic "$KAFKA_TOPIC")

if [[ "$PRETTY" -eq 0 ]]; then
  args+=(--property print.key=true --property print.timestamp=true --property print.headers=true --property key.separator=' | ')
fi

if [[ "$FROM_BEGINNING" -eq 1 ]]; then
  args+=(--from-beginning --timeout-ms "$TIMEOUT_MS")
fi

if [[ -n "$MAX_MESSAGES" ]]; then
  args+=(--max-messages "$MAX_MESSAGES")
fi

if [[ "$PRETTY" -eq 1 ]] && command -v jq >/dev/null; then
  kafka_exec "${args[@]}" | jq -c .
else
  kafka_exec "${args[@]}"
fi
