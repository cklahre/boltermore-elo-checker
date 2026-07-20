#!/usr/bin/env bash
# Run on the droplet (manually, systemd timer, or GitHub Actions) after matches JSON is updated.
# With PULL_MATCHES=1 (default via refresh.env on weekly timer), harvests new BCP matches first.
set -euo pipefail

ROOT="/opt/eloevent"
REPO="${ROOT}/repo"
# Optional fallback: cron may drop a single file into data/ — not tracked in repo.
MATCHES_LEGACY_DATA="${ROOT}/data/bcp-matches.json"
MATCHES_MANIFEST_DATA="${ROOT}/data/bcp-matches.manifest"
MATCHES_MANIFEST_REPO="${REPO}/bcp-matches.manifest"
LEADER="${ROOT}/data/leaderboard.json"
BIN="${ROOT}/bin/local-elo"
PULL_SCRIPT="${ROOT}/scripts/pull-matches.sh"

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

# Weekly automation: harvest + dated shard + manifest before Elo.
if [[ "${PULL_MATCHES:-0}" == "1" ]]; then
  if [[ ! -x "$PULL_SCRIPT" ]]; then
    echo "PULL_MATCHES=1 but missing $PULL_SCRIPT" >&2
    exit 1
  fi
  echo "Running pull-matches.sh ..."
  "$PULL_SCRIPT"
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
  echo "Or set PULL_MATCHES=1 / UPDATE_MATCHES_CMD / drop legacy ${MATCHES_LEGACY_DATA}." >&2
  exit 1
fi

echo "Restarting discord bot ..."
systemctl restart eloevent-discord-bot

echo "Done."
