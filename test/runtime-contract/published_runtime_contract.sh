#!/usr/bin/env bash
set -euo pipefail

image="${HERMES_RUNTIME_IMAGE:-ghcr.io/xmbshwll/hermes-agent-docker:latest}"
runtime_uid="${HERMES_RUNTIME_UID:-10001}"
workdir="$(mktemp -d)"
container_id=""

cleanup() {
  if [[ -n "$container_id" ]]; then
    docker exec -u 0 "$container_id" sh -lc 'chmod -R a+rwX /data/hermes || true' >/dev/null 2>&1 || true
    docker rm -f "$container_id" >/dev/null 2>&1 || true
  fi
  rm -rf "$workdir"
}
trap cleanup EXIT

log() {
  printf '==> %s\n' "$1"
}

log "Pulling published runtime image ${image}"
docker pull "$image" >/dev/null

cat >"$workdir/config.yaml" <<'EOF'
model: anthropic/claude-opus-4.1
terminal:
  backend: local
EOF

cat >"$workdir/gateway.json" <<'EOF'
{
  "platforms": {}
}
EOF

chmod 0777 "$workdir"

log "Starting published runtime image under the operator contract"
container_id="$(docker run -d \
  --user "${runtime_uid}:${runtime_uid}" \
  -e HERMES_HOME=/data/hermes \
  -v "$workdir:/data/hermes" \
  --entrypoint sh \
  "$image" \
  -lc 'hermes gateway'
)"

log "Checking binary availability, user, and writable paths"
docker exec "$container_id" sh -lc "
  test \"\$(id -u)\" = \"${runtime_uid}\"
  command -v hermes >/dev/null
  command -v bash >/dev/null
  test -w /tmp
  test -f /data/hermes/config.yaml
  test -f /data/hermes/gateway.json
"

log "Waiting for gateway runtime state files"
for _ in $(seq 1 60); do
  if docker exec "$container_id" sh -lc "
    test -f /data/hermes/gateway.pid &&
    test -f /data/hermes/gateway_state.json &&
    grep -Eq '\"gateway_state\"[[:space:]]*:[[:space:]]*\"running\"' /data/hermes/gateway_state.json
  " >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

log "Validating emitted runtime state"
docker exec "$container_id" sh -lc "
  test -f /data/hermes/gateway.pid
  test -f /data/hermes/gateway_state.json
  grep -Eq '\"pid\"[[:space:]]*:[[:space:]]*[0-9]+' /data/hermes/gateway.pid
  grep -Eq '\"pid\"[[:space:]]*:[[:space:]]*[0-9]+' /data/hermes/gateway_state.json
  grep -Eq '\"gateway_state\"[[:space:]]*:[[:space:]]*\"running\"' /data/hermes/gateway_state.json
"

log "Contract check passed for ${image}"
