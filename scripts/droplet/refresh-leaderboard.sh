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

# Services that also hold large match/leaderboard state in RAM on this droplet.
STOP_UNITS=(eloevent-discord-bot boltermore-web)
SWAPFILE="${SWAPFILE:-/swapfile}"
SWAP_SIZE="${SWAP_SIZE:-2G}"

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

ensure_swap() {
  if swapon --show | grep -q .; then
    echo "Swap already active:"
    swapon --show
    return 0
  fi
  if [[ -f "$SWAPFILE" ]]; then
    echo "Enabling existing swap ${SWAPFILE} ..."
    chmod 600 "$SWAPFILE" || true
    swapon "$SWAPFILE" || true
    return 0
  fi
  echo "No swap detected — creating ${SWAP_SIZE} ${SWAPFILE} (needed for Elo rebuild on small droplets) ..."
  # fallocate can fail on some filesystems; dd is the portable fallback
  if ! fallocate -l "$SWAP_SIZE" "$SWAPFILE" 2>/dev/null; then
    local count=2048
    if [[ "$SWAP_SIZE" =~ ^([0-9]+)G$ ]]; then
      count=$((BASH_REMATCH[1] * 1024))
    fi
    dd if=/dev/zero of="$SWAPFILE" bs=1M count="$count" status=progress
  fi
  chmod 600 "$SWAPFILE"
  mkswap "$SWAPFILE"
  swapon "$SWAPFILE"
  if ! grep -q "^${SWAPFILE} " /etc/fstab 2>/dev/null; then
    echo "${SWAPFILE} none swap sw 0 0" >>/etc/fstab
  fi
  swapon --show
}

stop_heavy_services() {
  echo "Stopping heavy services to free RAM for local-elo ..."
  free -h || true
  local u
  for u in "${STOP_UNITS[@]}"; do
    if systemctl is-active --quiet "$u" 2>/dev/null; then
      systemctl stop "$u" && echo "  stopped ${u}"
    else
      echo "  ${u} not running"
    fi
  done
  # Drop page cache hint (best-effort; needs root)
  sync || true
  echo 3 >/proc/sys/vm/drop_caches 2>/dev/null || true
  free -h || true
}

start_heavy_services() {
  echo "Starting services back up ..."
  local u
  for u in "${STOP_UNITS[@]}"; do
    if systemctl cat "$u" >/dev/null 2>&1; then
      systemctl start "$u" && echo "  started ${u}" || echo "  WARN: failed to start ${u}" >&2
    fi
  done
}

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

sync_website_leaderboard() {
  if [[ -d /opt/boltermore-web/data ]]; then
    mkdir -p "$WEB_ELO_DIR"
    cp "$LEADER" "$WEB_ELO"
    echo "Synced website Elo → ${WEB_ELO}"
  else
    echo "(skip website Elo sync — ${WEB_ELO_DIR} parent missing)"
  fi
}

ensure_swap
stop_heavy_services
trap start_heavy_services EXIT

# Leaderboard first. History chunks second so an OOM there does not block ratings.
run_local_elo() {
  local manifest="$1"
  echo "Running local-elo -out-json ..."
  "$BIN" -matches-manifest "$manifest" -out-json "$LEADER"
  sync_website_leaderboard

  if [[ -d /opt/boltermore-web/data ]]; then
    mkdir -p "$WEB_HISTORY_DIR"
    echo "Building website player-history chunks → ${WEB_HISTORY_DIR}"
    if "$BIN" -matches-manifest "$manifest" \
      -out-web-dir "$WEB_HISTORY_DIR" -recent-n 15 -web-page-size 100; then
      echo "Synced player history chunks → ${WEB_HISTORY_DIR}"
    else
      echo "WARN: player-history export failed (check RAM). Leaderboard was still updated." >&2
    fi
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
  echo "Running local-elo -out-json ..."
  "$BIN" -matches "$MATCHES_LEGACY_DATA" -out-json "$LEADER"
  sync_website_leaderboard
  if [[ -d /opt/boltermore-web/data ]]; then
    mkdir -p "$WEB_HISTORY_DIR"
    echo "Building website player-history chunks → ${WEB_HISTORY_DIR}"
    if "$BIN" -matches "$MATCHES_LEGACY_DATA" \
      -out-web-dir "$WEB_HISTORY_DIR" -recent-n 15 -web-page-size 100; then
      echo "Synced player history chunks → ${WEB_HISTORY_DIR}"
    else
      echo "WARN: player-history export failed (check RAM). Leaderboard was still updated." >&2
    fi
  fi
else
  echo "missing matches: need bcp-matches.manifest (+ listed shards)" >&2
  echo "  under ${ROOT}/data/ OR ${REPO}/ (committed shards + manifest from git)." >&2
  echo "Or set PULL_MATCHES=1 / UPDATE_MATCHES_CMD / drop legacy ${MATCHES_LEGACY_DATA}." >&2
  exit 1
fi

# EXIT trap restarts services (including discord bot)
echo "Done."
