#!/usr/bin/env bash
# Run on the droplet (manually or via GitHub Actions weekly job) after matches JSON is updated.
set -euo pipefail

ROOT="/opt/eloevent"
REPO="${ROOT}/repo"
MATCHES_LEGACY_DATA="${ROOT}/data/bcp-matches.json"
MATCHES_LEGACY_REPO="${REPO}/bcp-matches.json"
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
  echo "(using single file: $MATCHES_LEGACY_DATA)"
  "$BIN" -matches "$MATCHES_LEGACY_DATA" -out-json "$LEADER"
elif [[ -f "$MATCHES_LEGACY_REPO" ]]; then
  echo "(using single file from repo mirror: $MATCHES_LEGACY_REPO)"
  "$BIN" -matches "$MATCHES_LEGACY_REPO" -out-json "$LEADER"
else
  echo "missing matches input: manifest or monolith under ${ROOT}/data/ or manifest + shards committed under ${REPO}/" >&2
  echo "Copy your export(s) here, push them in git so deploy rsync fills ${REPO}/, or set UPDATE_MATCHES_CMD in env/refresh.env." >&2
  exit 1
fi

echo "Restarting discord bot ..."
systemctl restart eloevent-discord-bot

echo "Done."
