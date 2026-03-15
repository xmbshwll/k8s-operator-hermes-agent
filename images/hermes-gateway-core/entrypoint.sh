#!/usr/bin/env bash
set -euo pipefail

export HERMES_HOME="${HERMES_HOME:-/data/hermes}"
export HOME="${HOME:-/home/hermes}"
export HERMES_INSTALL_DIR="${HERMES_INSTALL_DIR:-/opt/hermes-src}"

mkdir -p \
  "$HERMES_HOME/cron" \
  "$HERMES_HOME/sessions" \
  "$HERMES_HOME/logs" \
  "$HERMES_HOME/pairing" \
  "$HERMES_HOME/hooks" \
  "$HERMES_HOME/image_cache" \
  "$HERMES_HOME/audio_cache" \
  "$HERMES_HOME/memories" \
  "$HERMES_HOME/skills" \
  "$HERMES_HOME/whatsapp/session"

if [ ! -f "$HERMES_HOME/.env" ]; then
  touch "$HERMES_HOME/.env"
fi

if [ ! -f "$HERMES_HOME/config.yaml" ] && [ -f "$HERMES_INSTALL_DIR/cli-config.yaml.example" ]; then
  cp "$HERMES_INSTALL_DIR/cli-config.yaml.example" "$HERMES_HOME/config.yaml"
fi

if [ ! -f "$HERMES_HOME/SOUL.md" ] && [ -f "$HERMES_INSTALL_DIR/SOUL.md" ]; then
  cp "$HERMES_INSTALL_DIR/SOUL.md" "$HERMES_HOME/SOUL.md"
fi

if [ -d "$HERMES_INSTALL_DIR/skills" ] && [ -z "$(find "$HERMES_HOME/skills" -mindepth 1 -print -quit 2>/dev/null || true)" ]; then
  cp -R "$HERMES_INSTALL_DIR/skills/." "$HERMES_HOME/skills/"
fi

exec "$@"
