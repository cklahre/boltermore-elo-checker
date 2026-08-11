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
  # Explicit PULL_MATCHES=0|1 on the command line wins over refresh.env.
  _PULL_MATCHES_OVERRIDE="${PULL_MATCHES-}"
  # shellcheck disable=SC1091
  set -a
  source "${ROOT}/env/refresh.env"
  set +a
  if [[ -n "${_PULL_MATCHES_OVERRIDE}" ]]; then
    PULL_MATCHES="${_PULL_MATCHES_OVERRIDE}"
  fi
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
WEB_ELO_DIR="/opt/boltermore-web/data/elo"
WEB_ELO="${WEB_ELO_DIR}/leaderboard.json"
WEB_HISTORY_DIR="${WEB_ELO_DIR}/web"

run_local_elo() {
  local manifest="$1"
  if [[ -d /opt/boltermore-web/data ]]; then
    mkdir -p "$WEB_HISTORY_DIR"
    "$BIN" -matches-manifest "$manifest" -out-json "$LEADER" \
      -out-web-dir "$WEB_HISTORY_DIR" -recent-n 15 -web-page-size 100
  else
    "$BIN" -matches-manifest "$manifest" -out-json "$LEADER"
  fi
}

if [[ -f "$MATCHES_MANIFEST_DATA" ]]; then
  echo "(using manifest: $MATCHES_MANIFEST_DATA)"
  run_local_elo "$MATCHES_MANIFEST_DATA"
elif [[ -f "$MATCHES_MANIFEST_REPO" ]]; then
  echo "(using manifest from repo mirror: $MATCHES_MANIFEST_REPO)"
  run_local_elo "$MATCHES_MANIFEST_REPO"
elif [[ -f "$MATCHES_LEGACY_DATA" ]]; then
  echo "(fallback single file — prefer manifest + shards: $MATCHES_LEGACY_DATA)"
  if [[ -d /opt/boltermore-web/data ]]; then
    mkdir -p "$WEB_HISTORY_DIR"
    "$BIN" -matches "$MATCHES_LEGACY_DATA" -out-json "$LEADER" \
      -out-web-dir "$WEB_HISTORY_DIR" -recent-n 15 -web-page-size 100
  else
    "$BIN" -matches "$MATCHES_LEGACY_DATA" -out-json "$LEADER"
  fi
else
  echo "missing matches: need bcp-matches.manifest (+ listed shards)" >&2
  echo "  under ${ROOT}/data/ OR ${REPO}/ (committed shards + manifest from git)." >&2
  echo "Or set PULL_MATCHES=1 / UPDATE_MATCHES_CMD / drop legacy ${MATCHES_LEGACY_DATA}." >&2
  exit 1
fi

echo "Restarting discord bot ..."
systemctl restart eloevent-discord-bot

# Keep the website Elo page in sync with the same leaderboard snapshot
if [[ -d /opt/boltermore-web/data ]]; then
  mkdir -p "$WEB_ELO_DIR"
  cp "$LEADER" "$WEB_ELO"
  echo "Synced website Elo → ${WEB_ELO}"
  if [[ -d "$WEB_HISTORY_DIR" ]]; then
    echo "Synced player history chunks → ${WEB_HISTORY_DIR}"
  fi
else
  echo "(skip website Elo sync — ${WEB_ELO_DIR} parent missing)"
fi

echo "Done."
