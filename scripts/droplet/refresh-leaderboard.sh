#!/usr/bin/env bash
# Run on the droplet (manually or via GitHub Actions weekly job) after matches JSON is updated.
set -euo pipefail

ROOT="/opt/eloevent"
MATCHES_LEGACY="${ROOT}/data/bcp-matches.json"
MATCHES_MANIFEST="${ROOT}/data/bcp-matches.manifest"
LEADER="${ROOT}/data/leaderboard.json"
BIN="${ROOT}/bin/local-elo"

if [[ ! -x "$BIN" ]]; then
  echo "missing $BIN (deploy binaries from GitHub Actions first)" >&2
  exit 1
fi

if [[ -f "${ROOT}/env/refresh.env" ]]; then
  # shellcheck disable=SC1091
  set -a
  source "${ROOT}/env/refresh.env"
  set +a
fi

if [[ -n "${UPDATE_MATCHES_CMD:-}" ]]; then
  echo "Running UPDATE_MATCHES_CMD ..."
  bash -c "$UPDATE_MATCHES_CMD"
fi

echo "Regenerating leaderboard from matches ..."
if [[ -f "$MATCHES_MANIFEST" ]]; then
  echo "(using manifest: $MATCHES_MANIFEST)"
  "$BIN" -matches-manifest "$MATCHES_MANIFEST" -out-json "$LEADER"
elif [[ -f "$MATCHES_LEGACY" ]]; then
  echo "(using single file: $MATCHES_LEGACY)"
  "$BIN" -matches "$MATCHES_LEGACY" -out-json "$LEADER"
else
  echo "missing matches input: either $MATCHES_MANIFEST (multi-shard list) or $MATCHES_LEGACY" >&2
  echo "Copy your export(s) here or set UPDATE_MATCHES_CMD in env/refresh.env (e.g. curl from Spaces)." >&2
  exit 1
fi

echo "Restarting discord bot ..."
systemctl restart eloevent-discord-bot

echo "Done."
