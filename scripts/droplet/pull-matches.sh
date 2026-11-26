#!/usr/bin/env bash
# Harvest new BCP events since the latest dated shard, export a new shard into
# /opt/eloevent/data/, and regenerate bcp-matches.manifest there.
#
# Intended for Monday-morning systemd timer (see eloevent-refresh-leaderboard.timer)
# or: PULL_MATCHES=1 /opt/eloevent/scripts/refresh-leaderboard.sh
set -euo pipefail

ROOT="/opt/eloevent"
DATA="${ROOT}/data"
REPO="${ROOT}/repo"
BIN="${ROOT}/bin"
TODAY="$(date -u +%Y-%m-%d)"
NEW_SHARD="${DATA}/bcp-matches-${TODAY}.json"
LOG_PREFIX="pull-matches"

HARVEST_BIN="${BIN}/bcp-harvest-events"
EXPORT_BIN="${BIN}/bcp-export-matches"
MANIFEST_BIN="${BIN}/bcp-matches-manifest"

if [[ -f "${ROOT}/env/refresh.env" ]]; then
	# shellcheck disable=SC1091
	set -a
	source "${ROOT}/env/refresh.env"
	set +a
fi

EXPORT_SLEEP_MS="${EXPORT_SLEEP_MS:-300}"
HARVEST_MIN_INTERVAL_MS="${HARVEST_MIN_INTERVAL_MS:-350}"
# GT-oriented gates (same as bcp-harvest / bcp-export-matches flags). RTTs/leagues out.
MIN_PLAYERS="${MIN_PLAYERS:-20}"
MIN_ROUNDS="${MIN_ROUNDS:-5}"
MAX_SPAN_DAYS="${MAX_SPAN_DAYS:-7}"
EXCLUDE_NAME="${EXCLUDE_NAME:-league,season,ladder,rtt,team,teams}"
# Re-export events that already appear in shards with games in this many UTC days.
# Catches mid-tournament harvests that only got early rounds (pairing_id dedupe keeps old rows).
REEXPORT_LOOKBACK_DAYS="${REEXPORT_LOOKBACK_DAYS:-21}"
# Optional comma/space-separated BCP event ids to force into this pull (one-shot heal).
REEXPORT_EVENT_IDS="${REEXPORT_EVENT_IDS:-}"

log() { echo "${LOG_PREFIX}: $*"; }
die() { echo "${LOG_PREFIX}: $*" >&2; exit 1; }

for b in "$HARVEST_BIN" "$EXPORT_BIN" "$MANIFEST_BIN"; do
	[[ -x "$b" ]] || die "missing executable $b (redeploy from GitHub Actions)"
done

mkdir -p "$DATA"
# Per-run workdir: a shared path gets deleted out from under overlapping runs.
WORKDIR="$(mktemp -d "${DATA}/.pull-work.XXXXXX")"
trap 'rm -rf "$WORKDIR"' EXIT
IDS_FILE="${WORKDIR}/event-ids.txt"

# Seed data/ from repo mirror once so a data/manifest never drops historical shards.
seed_from_repo() {
	if [[ -f "${DATA}/bcp-matches.manifest" ]]; then
		return 0
	fi
	if [[ ! -f "${REPO}/bcp-matches.manifest" ]]; then
		return 0
	fi
	log "seeding ${DATA}/ from repo manifest ${REPO}/bcp-matches.manifest"
	local man_dir raw trimmed entry src bn
	man_dir="$REPO"
	cp "${REPO}/bcp-matches.manifest" "${DATA}/bcp-matches.manifest"
	while IFS= read -r raw || [[ -n "${raw:-}" ]]; do
		raw="${raw//$'\r'/}"
		trimmed="${raw#"${raw%%[![:space:]]*}"}"
		trimmed="${trimmed%"${trimmed##*[![:space:]]}"}"
		[[ -z "$trimmed" ]] && continue
		[[ "${trimmed:0:1}" == '#' ]] && continue
		entry="${trimmed%%#*}"
		entry="${entry%"${entry##*[![:space:]]}"}"
		if [[ "$entry" = /* ]]; then
			src="$entry"
		else
			src="${man_dir}/${entry}"
		fi
		[[ -f "$src" ]] || die "seed shard missing: $entry → $src"
		bn="$(basename "$entry")"
		if [[ ! -f "${DATA}/${bn}" ]]; then
			cp "$src" "${DATA}/${bn}"
			log "copied shard ${bn}"
		fi
	done <"${REPO}/bcp-matches.manifest"
}

newest_shard_date() {
	local f base date
	local latest=""
	shopt -s nullglob
	for f in "${DATA}"/bcp-matches-[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9].json; do
		base="$(basename "$f" .json)"
		date="${base#bcp-matches-}"
		if [[ "$date" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]]; then
			if [[ -z "$latest" || "$date" > "$latest" ]]; then
				latest="$date"
			fi
		fi
	done
	shopt -u nullglob
	echo "$latest"
}

day_after() {
	# GNU date (Ubuntu droplet); falls back to Python if needed.
	local d="$1"
	if date -u -d "${d} +1 day" +%Y-%m-%d 2>/dev/null; then
		return 0
	fi
	python3 -c "from datetime import date,timedelta; print((date.fromisoformat('${d}')+timedelta(days=1)).isoformat())"
}

# Collect event ids already in shards whose games fall on/after cutoff (for incomplete-event heal).
collect_lookback_event_ids() {
	local cutoff="$1"
	local out_file="$2"
	python3 - "$DATA" "$cutoff" "$out_file" <<'PY'
import json, sys
from pathlib import Path

data_dir, cutoff, out_path = sys.argv[1:4]
ids = set()
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
        day = str(r.get("date") or "")[:10]
        if eid and day >= cutoff:
            ids.add(eid)
Path(out_path).write_text("".join(f"{i}\n" for i in sorted(ids)), encoding="utf-8")
print(len(ids))
PY
}

merge_id_files() {
	local out="$1"
	shift
	python3 - "$out" "$@" <<'PY'
import sys
from pathlib import Path
out = Path(sys.argv[1])
ids = []
seen = set()
for path in sys.argv[2:]:
    p = Path(path)
    if not p.is_file():
        continue
    for line in p.read_text(encoding="utf-8").splitlines():
        eid = line.strip()
        if not eid or eid.startswith("#") or eid in seen:
            continue
        seen.add(eid)
        ids.append(eid)
out.write_text("".join(f"{i}\n" for i in ids), encoding="utf-8")
print(len(ids))
PY
}

seed_from_repo

SINCE="${SINCE:-}"
if [[ -z "$SINCE" ]]; then
	latest="$(newest_shard_date)"
	if [[ -n "$latest" ]]; then
		SINCE="$(day_after "$latest")"
		log "newest shard date ${latest} → -since ${SINCE}"
	else
		die "no bcp-matches-YYYY-MM-DD.json in ${DATA}/ and SINCE unset"
	fi
else
	log "using SINCE=${SINCE} from env"
fi

: >"$IDS_FILE"
log "harvesting GT events since ${SINCE} (min-players=${MIN_PLAYERS} min-rounds=${MIN_ROUNDS} max-span-days=${MAX_SPAN_DAYS} exclude-name=${EXCLUDE_NAME}) ..."
"$HARVEST_BIN" \
	-since "$SINCE" \
	-min-players "$MIN_PLAYERS" \
	-min-rounds "$MIN_ROUNDS" \
	-max-span-days "$MAX_SPAN_DAYS" \
	-exclude-name "$EXCLUDE_NAME" \
	-min-interval-ms "$HARVEST_MIN_INTERVAL_MS" \
	-out-ids "$IDS_FILE" \
	-out-events-json "${WORKDIR}/events.json" || true

LOOKBACK_IDS="${WORKDIR}/lookback-ids.txt"
: >"$LOOKBACK_IDS"
if [[ "${REEXPORT_LOOKBACK_DAYS}" =~ ^[0-9]+$ ]] && [[ "$REEXPORT_LOOKBACK_DAYS" -gt 0 ]]; then
	if date -u -d "${REEXPORT_LOOKBACK_DAYS} days ago" +%Y-%m-%d >/dev/null 2>&1; then
		LOOKBACK_CUTOFF="$(date -u -d "${REEXPORT_LOOKBACK_DAYS} days ago" +%Y-%m-%d)"
	else
		LOOKBACK_CUTOFF="$(python3 -c "from datetime import date,timedelta; print((date.today()-timedelta(days=int('${REEXPORT_LOOKBACK_DAYS}'))).isoformat())")"
	fi
	n_lookback="$(collect_lookback_event_ids "$LOOKBACK_CUTOFF" "$LOOKBACK_IDS")"
	log "lookback re-export: ${n_lookback} event ids with games on/after ${LOOKBACK_CUTOFF}"
fi

FORCE_IDS="${WORKDIR}/force-ids.txt"
: >"$FORCE_IDS"
if [[ -n "${REEXPORT_EVENT_IDS}" ]]; then
	python3 - "$FORCE_IDS" "$REEXPORT_EVENT_IDS" <<'PY'
import sys
from pathlib import Path
out, raw = Path(sys.argv[1]), sys.argv[2]
ids = []
for part in raw.replace(",", " ").split():
    part = part.strip()
    if part:
        ids.append(part)
out.write_text("".join(f"{i}\n" for i in ids), encoding="utf-8")
print(len(ids))
PY
	log "forced re-export ids: $(wc -l <"$FORCE_IDS" | tr -d ' ')"
fi

MERGED_IDS="${WORKDIR}/merged-ids.txt"
n_ids="$(merge_id_files "$MERGED_IDS" "$IDS_FILE" "$LOOKBACK_IDS" "$FORCE_IDS")"
n_ids="${n_ids:-0}"
if [[ "$n_ids" -eq 0 ]]; then
	log "no GT events to export (harvest empty, lookback empty); leaving shards unchanged"
	exit 0
fi
log "exporting pairings for ${n_ids} events (new + lookback heal) ..."

set +e
"$EXPORT_BIN" \
	-events-file "$MERGED_IDS" \
	-out "$NEW_SHARD" \
	-continue-on-error \
	-sleep-ms "$EXPORT_SLEEP_MS" \
	-min-players "$MIN_PLAYERS" \
	-min-rounds "$MIN_ROUNDS" \
	-max-span-days "$MAX_SPAN_DAYS" \
	-exclude-name "$EXCLUDE_NAME"
export_rc=$?
set -e

if [[ "$export_rc" -ne 0 ]]; then
	if [[ -f "$NEW_SHARD" ]]; then
		log "export exited ${export_rc} but wrote ${NEW_SHARD}; continuing"
	else
		log "export produced no games (rc=${export_rc}); leaving shards unchanged"
		exit 0
	fi
fi

if [[ ! -f "$NEW_SHARD" ]]; then
	log "no new shard written; leaving shards unchanged"
	exit 0
fi

log "regenerating manifest from dated shards in ${DATA}/"
(
	cd "$DATA"
	"$MANIFEST_BIN" -out bcp-matches.manifest -sort name
)

log "wrote ${NEW_SHARD} and updated ${DATA}/bcp-matches.manifest"
