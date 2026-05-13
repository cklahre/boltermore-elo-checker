#!/usr/bin/env bash
# Run on the droplet (manually or via GitHub Actions weekly job) after matches JSON is updated.
set -euo pipefail

ROOT="/opt/eloevent"
MATCHES="${ROOT}/data/bcp-matches.json"
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

if [[ ! -f "$MATCHES" ]]; then
  echo "missing matches file: $MATCHES" >&2
  echo "Copy your export here or set UPDATE_MATCHES_CMD in env/refresh.env (e.g. curl from Spaces)." >&2
  exit 1
fi

echo "Regenerating leaderboard from matches ..."
"$BIN" -matches "$MATCHES" -out-json "$LEADER"

echo "Restarting discord bot ..."
systemctl restart eloevent-discord-bot

echo "Done."
