#!/usr/bin/env bash
# Smoke test for the benzhi AAC quality-closure backend. It builds the server,
# starts it on a local port with a temporary state file, probes the health and
# public API endpoints, then shuts down and cleans up. It performs no external
# network access and does not call `go test`.
set -euo pipefail

BIN_DIR="$(mktemp -d)"
STATE_DIR="$(mktemp -d)"
PORT="${BENZHI_SMOKE_PORT:-18080}"
BASE="http://127.0.0.1:${PORT}"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${BIN_DIR}" "${STATE_DIR}"
}
trap cleanup EXIT

echo "building server binary"
go build -o "${BIN_DIR}/server" ./cmd/server

echo "starting server at ${BASE}"
STATE_PATH="${STATE_DIR}/state.json" ADDR="127.0.0.1:${PORT}" "${BIN_DIR}/server" &
SERVER_PID=$!

# Wait for the server to become ready.
ready=0
for _ in $(seq 1 100); do
  if curl -s -o /dev/null "${BASE}/healthz"; then
    ready=1
    break
  fi
  sleep 0.1
done
if [[ "${ready}" != "1" ]]; then
  echo "server did not become ready" >&2
  exit 1
fi

# Health probe: capture the body in a variable (never pipe curl into grep).
health="$(curl -s "${BASE}/healthz")"
if [[ "${health}" != *'"status":"ok"'* ]]; then
  echo "unexpected health response: ${health}" >&2
  exit 1
fi
echo "health probe ok"

# Public API probe: creating a task with a stale recipe hash must return a
# stable STALE_RULE error without leaving any residue.
create_code="$(curl -s -o "${STATE_DIR}/create.json" -w '%{http_code}' \
  -X POST "${BASE}/v1/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"operation_id":"smoke-1","factory":"f","production_batch":"p","rule_version":"v1","recipe_hash":"bogus","body_ids":["b1"],"batches":[{"class":"cement","batch":"cem-1"}]}')"
create_body="$(cat "${STATE_DIR}/create.json")"
if [[ "${create_code}" != "400" ]]; then
  echo "expected HTTP 400 for stale rule, got ${create_code}: ${create_body}" >&2
  exit 1
fi
if [[ "${create_body}" != *'STALE_RULE'* ]]; then
  echo "expected STALE_RULE reason in body: ${create_body}" >&2
  exit 1
fi
echo "create-task stale-rule probe ok"

# Query probe: an unknown task returns a stable 400 error envelope.
query_code="$(curl -s -o "${STATE_DIR}/query.json" -w '%{http_code}' "${BASE}/v1/tasks/does-not-exist")"
if [[ "${query_code}" != "400" ]]; then
  echo "expected HTTP 400 for unknown task, got ${query_code}" >&2
  exit 1
fi
echo "unknown-task query probe ok"

# Persistence probe: a successfully locked task is durably written to the state
# file and must survive a server restart, proving the persistence implementation.
persist_code="$(curl -s -o "${STATE_DIR}/persist.json" -w '%{http_code}' \
  -X POST "${BASE}/v1/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"operation_id":"smoke-persist","factory":"f","production_batch":"p-persist","rule_version":"v1","recipe_hash":"2c5abd6e5d2c279f36b43159250f91b7cce51fc02238254e82cc746b3fa63efa","body_ids":["b1"],"batches":[{"class":"cement","batch":"cem-1"}],"raw_grams":{"cement":1000}}')"
if [[ "${persist_code}" != "201" ]]; then
  echo "expected HTTP 201 for valid task create, got ${persist_code}: $(cat "${STATE_DIR}/persist.json")" >&2
  exit 1
fi

# Restart the server over the same state file and verify the task is recovered.
kill "${SERVER_PID}" 2>/dev/null || true
wait "${SERVER_PID}" 2>/dev/null || true
STATE_PATH="${STATE_DIR}/state.json" ADDR="127.0.0.1:${PORT}" "${BIN_DIR}/server" &
SERVER_PID=$!
ready=0
for _ in $(seq 1 100); do
  if curl -s -o /dev/null "${BASE}/healthz"; then
    ready=1
    break
  fi
  sleep 0.1
done
if [[ "${ready}" != "1" ]]; then
  echo "server did not become ready after restart" >&2
  exit 1
fi
recovered="$(curl -s "${BASE}/v1/tasks/f-p-persist")"
if [[ "${recovered}" != *'"id":"f-p-persist"'* ]]; then
  echo "task not recovered after restart: ${recovered}" >&2
  exit 1
fi
echo "persistence restart-recovery probe ok"

echo "smoke test passed"
