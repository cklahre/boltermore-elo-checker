#!/usr/bin/env bash
# Harvest new BCP events since the latest dated shard, export a new shard into
# /opt/eloevent/data/, and regenerate bcp-matches.manifest there.
#
# Intended for Monday-afternoon systemd timer (see eloevent-refresh-leaderboard.timer)
# or: PULL_MATCHES=1 /opt/eloevent/scripts/refresh-leaderboard.sh
set -euo pipefail

ROOT="/opt/eloevent"
DATA="${ROOT}/data"
REPO="${ROOT}/repo"
BIN="${ROOT}/bin"
WORKDIR="${DATA}/.pull-work"
TODAY="$(date -u +%Y-%m-%d)"
NEW_SHARD="${DATA}/bcp-matches-${TODAY}.json"
IDS_FILE="${WORKDIR}/event-ids.txt"
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

log() { echo "${LOG_PREFIX}: $*"; }
die() { echo "${LOG_PREFIX}: $*" >&2; exit 1; }

for b in "$HARVEST_BIN" "$EXPORT_BIN" "$MANIFEST_BIN"; do
	[[ -x "$b" ]] || die "missing executable $b (redeploy from GitHub Actions)"
done

mkdir -p "$DATA" "$WORKDIR"

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

log "harvesting GT events since ${SINCE} (min-players=${MIN_PLAYERS} min-rounds=${MIN_ROUNDS} max-span-days=${MAX_SPAN_DAYS} exclude-name=${EXCLUDE_NAME}) ..."
"$HARVEST_BIN" \
	-since "$SINCE" \
	-min-players "$MIN_PLAYERS" \
	-min-rounds "$MIN_ROUNDS" \
	-max-span-days "$MAX_SPAN_DAYS" \
	-exclude-name "$EXCLUDE_NAME" \
	-min-interval-ms "$HARVEST_MIN_INTERVAL_MS" \
	-out-ids "$IDS_FILE" \
	-out-events-json "${WORKDIR}/events.json"

n_ids="$(grep -cve '^[[:space:]]*$' "$IDS_FILE" 2>/dev/null || true)"
n_ids="${n_ids:-0}"
if [[ "$n_ids" -eq 0 ]]; then
	log "no GT events since ${SINCE}; leaving shards unchanged"
	rm -rf "$WORKDIR"
	exit 0
fi
log "discovered ${n_ids} GT events; exporting pairings ..."

set +e
"$EXPORT_BIN" \
	-events-file "$IDS_FILE" \
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
		rm -rf "$WORKDIR"
		exit 0
	fi
fi

if [[ ! -f "$NEW_SHARD" ]]; then
	log "no new shard written; leaving shards unchanged"
	rm -rf "$WORKDIR"
	exit 0
fi

log "regenerating manifest from dated shards in ${DATA}/"
(
	cd "$DATA"
	"$MANIFEST_BIN" -out bcp-matches.manifest -sort name
)

rm -rf "$WORKDIR"
log "wrote ${NEW_SHARD} and updated ${DATA}/bcp-matches.manifest"
