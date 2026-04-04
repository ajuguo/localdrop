#!/bin/zsh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
BIN_PATH="${PROJECT_DIR}/bin/localdrop"
RUN_DIR="${PROJECT_DIR}/run"
PID_FILE="${RUN_DIR}/localdrop.pid"
LOG_FILE="${RUN_DIR}/localdrop.log"

mkdir -p "${RUN_DIR}"

find_running_pid() {
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid="$(<"${PID_FILE}")"
    if [[ -n "${pid}" ]] && kill -0 "${pid}" 2>/dev/null; then
      echo "${pid}"
      return 0
    fi
    rm -f "${PID_FILE}"
  fi

  local pid
  pid="$(pgrep -f "^${BIN_PATH}$" || true)"
  if [[ -n "${pid}" ]]; then
    echo "${pid}"
    return 0
  fi

  return 1
}

start_service() {
  if [[ ! -x "${BIN_PATH}" ]]; then
    echo "Binary not found or not executable: ${BIN_PATH}"
    exit 1
  fi

  if pid="$(find_running_pid)"; then
    echo "LocalDrop is already running (PID: ${pid})"
    exit 0
  fi

  nohup "${BIN_PATH}" >>"${LOG_FILE}" 2>&1 &
  local pid=$!
  echo "${pid}" > "${PID_FILE}"
  sleep 1

  if kill -0 "${pid}" 2>/dev/null; then
    echo "LocalDrop started in background"
    echo "PID: ${pid}"
    echo "Log: ${LOG_FILE}"
  else
    echo "Failed to start LocalDrop"
    rm -f "${PID_FILE}"
    exit 1
  fi
}

stop_service() {
  if ! pid="$(find_running_pid)"; then
    echo "LocalDrop is not running"
    exit 0
  fi

  kill "${pid}" 2>/dev/null || true

  for _ in {1..10}; do
    if ! kill -0 "${pid}" 2>/dev/null; then
      rm -f "${PID_FILE}"
      echo "LocalDrop stopped"
      exit 0
    fi
    sleep 1
  done

  echo "Process did not exit in time, forcing stop..."
  kill -9 "${pid}" 2>/dev/null || true
  rm -f "${PID_FILE}"
  echo "LocalDrop force stopped"
}

status_service() {
  if pid="$(find_running_pid)"; then
    echo "LocalDrop is running (PID: ${pid})"
    echo "Binary: ${BIN_PATH}"
    echo "Log: ${LOG_FILE}"
  else
    echo "LocalDrop is not running"
  fi
}

tail_log() {
  touch "${LOG_FILE}"
  tail -f "${LOG_FILE}"
}

usage() {
  cat <<EOF
Usage: $(basename "$0") {start|stop|restart|status|log}

  start    Start LocalDrop in background
  stop     Stop the running LocalDrop process
  restart  Restart LocalDrop
  status   Show whether LocalDrop is running
  log      Follow the LocalDrop log file
EOF
}

case "${1:-}" in
  start)
    start_service
    ;;
  stop)
    stop_service
    ;;
  restart)
    stop_service || true
    start_service
    ;;
  status)
    status_service
    ;;
  log)
    tail_log
    ;;
  *)
    usage
    exit 1
    ;;
esac
