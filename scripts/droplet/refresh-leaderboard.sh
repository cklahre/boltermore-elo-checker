#!/usr/bin/env bash
# Run on the droplet (manually or via GitHub Actions weekly job) after matches JSON is updated.
set -euo pipefail

ROOT="/opt/eloevent"
REPO="${ROOT}/repo"
# Optional fallback: cron / UPDATE_MATCHES_CMD may drop a single file into data/ — not tracked in repo.
MATCHES_LEGACY_DATA="${ROOT}/data/bcp-matches.json"
MATCHES_MANIFEST_DATA="${ROOT}/data/bcp-matches.manifest"
MATCHES_MANIFEST_REPO="${REPO}/bcp-matches.manifest"
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
if [[ -f "$MATCHES_MANIFEST_DATA" ]]; then
  echo "(using manifest: $MATCHES_MANIFEST_DATA)"
  "$BIN" -matches-manifest "$MATCHES_MANIFEST_DATA" -out-json "$LEADER"
elif [[ -f "$MATCHES_MANIFEST_REPO" ]]; then
  echo "(using manifest from repo mirror: $MATCHES_MANIFEST_REPO)"
  "$BIN" -matches-manifest "$MATCHES_MANIFEST_REPO" -out-json "$LEADER"
elif [[ -f "$MATCHES_LEGACY_DATA" ]]; then
  echo "(fallback single file — prefer manifest + shards: $MATCHES_LEGACY_DATA)"
  "$BIN" -matches "$MATCHES_LEGACY_DATA" -out-json "$LEADER"
else
  echo "missing matches: need bcp-matches.manifest (+ listed shards)" >&2
  echo "  under ${ROOT}/data/ OR ${REPO}/ (committed shards + manifest from git)." >&2
  echo "Or set UPDATE_MATCHES_CMD / drop legacy ${MATCHES_LEGACY_DATA} from automation." >&2
  exit 1
fi

echo "Restarting discord bot ..."
systemctl restart eloevent-discord-bot

echo "Done."
