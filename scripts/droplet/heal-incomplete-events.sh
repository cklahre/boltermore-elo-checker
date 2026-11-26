#!/usr/bin/env bash
# Find BCP events in existing shards that look mid-tournament incomplete
# (most players share the same low game count), re-export them, append a heal
# shard, and rebuild the leaderboard.
#
# Example (on droplet):
#   bash /opt/eloevent/scripts/heal-incomplete-events.sh
#   MAX_GAMES_PER_PLAYER=4 bash /opt/eloevent/scripts/heal-incomplete-events.sh
set -euo pipefail

ROOT="/opt/eloevent"
DATA="${ROOT}/data"
BIN="${ROOT}/bin"
TODAY="$(date -u +%Y-%m-%d)"
HEAL_SHARD="${DATA}/bcp-matches-heal-${TODAY}.json"
LOG_PREFIX="heal-incomplete"

EXPORT_BIN="${BIN}/bcp-export-matches"
MANIFEST_BIN="${BIN}/bcp-matches-manifest"
REFRESH="${ROOT}/scripts/refresh-leaderboard.sh"

EXPORT_SLEEP_MS="${EXPORT_SLEEP_MS:-300}"
MIN_PLAYERS="${MIN_PLAYERS:-20}"
MIN_ROUNDS="${MIN_ROUNDS:-5}"
MAX_SPAN_DAYS="${MAX_SPAN_DAYS:-7}"
EXCLUDE_NAME="${EXCLUDE_NAME:-league,season,ladder,rtt,team,teams}"
# Flag events where ≥ SHARE of players have exactly the same game count ≤ MAX_GAMES.
MAX_GAMES_PER_PLAYER="${MAX_GAMES_PER_PLAYER:-4}"
MIN_PLAYER_SHARE="${MIN_PLAYER_SHARE:-0.75}"
MIN_EVENT_PLAYERS="${MIN_EVENT_PLAYERS:-40}"
SKIP_REFRESH="${SKIP_REFRESH:-0}"

if [[ -f "${ROOT}/env/refresh.env" ]]; then
	# shellcheck disable=SC1091
	set -a
	source "${ROOT}/env/refresh.env"
	set +a
fi

log() { echo "${LOG_PREFIX}: $*"; }
die() { echo "${LOG_PREFIX}: $*" >&2; exit 1; }

[[ -x "$EXPORT_BIN" ]] || die "missing $EXPORT_BIN"
[[ -x "$MANIFEST_BIN" ]] || die "missing $MANIFEST_BIN"
[[ -f "${DATA}/bcp-matches.manifest" ]] || die "missing ${DATA}/bcp-matches.manifest"

WORKDIR="$(mktemp -d "${DATA}/.heal-work.XXXXXX")"
trap 'rm -rf "$WORKDIR"' EXIT
IDS_FILE="${WORKDIR}/incomplete-ids.txt"

log "scanning shards for incomplete-looking events ..."
python3 - "$DATA" "$IDS_FILE" "$MAX_GAMES_PER_PLAYER" "$MIN_PLAYER_SHARE" "$MIN_EVENT_PLAYERS" <<'PY'
import json, sys
from collections import Counter, defaultdict
from pathlib import Path

data_dir, out_path, max_games_s, share_s, min_players_s = sys.argv[1:6]
max_games = int(max_games_s)
min_share = float(share_s)
min_players = int(min_players_s)

by_event = defaultdict(Counter)  # event_id -> Counter(player -> games)
names = {}

for path in sorted(Path(data_dir).glob("bcp-matches-*.json")):
    try:
        rows = json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        continue
    if isinstance(rows, dict):
        rows = rows.get("matches") or rows.get("games") or []
    if not isinstance(rows, list):
        continue
    for r in rows:
        if not isinstance(r, dict):
            continue
        eid = str(r.get("event_id") or "").strip()
        if not eid:
            continue
        a = str(r.get("a") or "").strip()
        b = str(r.get("b") or "").strip()
        if a:
            by_event[eid][a] += 1
        if b:
            by_event[eid][b] += 1
        en = str(r.get("event_name") or "").strip()
        if en and eid not in names:
            names[eid] = en

flagged = []
for eid, pc in by_event.items():
    n = len(pc)
    if n < min_players:
        continue
    dist = Counter(pc.values())
    mode_games, mode_count = dist.most_common(1)[0]
    if mode_games > max_games:
        continue
    if mode_count / n < min_share:
        continue
    flagged.append((eid, names.get(eid, ""), n, mode_games, mode_count))

flagged.sort(key=lambda x: (-x[3], -x[2], x[0]))
Path(out_path).write_text("".join(f"{eid}\n" for eid, *_ in flagged), encoding="utf-8")
for eid, name, n, mode_games, mode_count in flagged:
    label = name or eid
    print(f"  {eid}  players={n}  mode_games={mode_games} ({mode_count}/{n})  {label}")
print(len(flagged))
PY

n_ids="$(grep -cve '^[[:space:]]*$' "$IDS_FILE" 2>/dev/null || true)"
n_ids="${n_ids:-0}"
if [[ "$n_ids" -eq 0 ]]; then
	log "no incomplete-looking events found"
	exit 0
fi
log "re-exporting ${n_ids} events → ${HEAL_SHARD}"

set +e
"$EXPORT_BIN" \
	-events-file "$IDS_FILE" \
	-out "$HEAL_SHARD" \
	-continue-on-error \
	-sleep-ms "$EXPORT_SLEEP_MS" \
	-min-players "$MIN_PLAYERS" \
	-min-rounds "$MIN_ROUNDS" \
	-max-span-days "$MAX_SPAN_DAYS" \
	-exclude-name "$EXCLUDE_NAME"
export_rc=$?
set -e

if [[ ! -f "$HEAL_SHARD" ]]; then
	die "export produced no shard (rc=${export_rc})"
fi

log "regenerating manifest ..."
(
	cd "$DATA"
	"$MANIFEST_BIN" -out bcp-matches.manifest -sort name
)

if [[ "$SKIP_REFRESH" == "1" ]]; then
	log "SKIP_REFRESH=1 — heal shard written; run refresh-leaderboard.sh yourself"
	exit 0
fi

[[ -x "$REFRESH" ]] || die "missing $REFRESH"
log "rebuilding leaderboards (PULL_MATCHES=0) ..."
PULL_MATCHES=0 "$REFRESH"
log "done"
