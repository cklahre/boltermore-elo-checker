#!/usr/bin/env bash
# One-shot backfill: harvest BCP events from the earliest date already in your
# shards with MIN_PLAYERS (default 20), export only event IDs missing from
# existing shards, append a backfill shard, rewrite the manifest (old shards
# first, backfill last), then rebuild the leaderboard.
#
# Usage (on droplet after deploy):
#   bash /opt/eloevent/scripts/backfill-min-players.sh
#
# Optional env (same knobs as pull-matches.sh / refresh.env):
#   MIN_PLAYERS MIN_ROUNDS MAX_SPAN_DAYS EXCLUDE_NAME
#   EXPORT_SLEEP_MS HARVEST_MIN_INTERVAL_MS
#   SKIP_REFRESH=1   # write shard+manifest only; skip refresh-leaderboard.sh
set -euo pipefail

ROOT="/opt/eloevent"
DATA="${ROOT}/data"
BIN="${ROOT}/bin"
WORKDIR="${DATA}/.backfill-min-players-work"
TODAY="$(date -u +%Y-%m-%d)"
BACKFILL_SHARD_NAME="bcp-matches-backfill-min20.json"
BACKFILL_SHARD="${DATA}/${BACKFILL_SHARD_NAME}"
MANIFEST="${DATA}/bcp-matches.manifest"
LOG_PREFIX="backfill-min-players"

HARVEST_BIN="${BIN}/bcp-harvest-events"
EXPORT_BIN="${BIN}/bcp-export-matches"
REFRESH_SCRIPT="${ROOT}/scripts/refresh-leaderboard.sh"

if [[ -f "${ROOT}/env/refresh.env" ]]; then
	# shellcheck disable=SC1091
	set -a
	source "${ROOT}/env/refresh.env"
	set +a
fi

EXPORT_SLEEP_MS="${EXPORT_SLEEP_MS:-300}"
HARVEST_MIN_INTERVAL_MS="${HARVEST_MIN_INTERVAL_MS:-350}"
MIN_PLAYERS="${MIN_PLAYERS:-20}"
MIN_ROUNDS="${MIN_ROUNDS:-5}"
MAX_SPAN_DAYS="${MAX_SPAN_DAYS:-7}"
EXCLUDE_NAME="${EXCLUDE_NAME:-league,season,ladder,rtt,team,teams}"
SKIP_REFRESH="${SKIP_REFRESH:-0}"

log() { echo "${LOG_PREFIX}: $*"; }
die() { echo "${LOG_PREFIX}: $*" >&2; exit 1; }

for b in "$HARVEST_BIN" "$EXPORT_BIN"; do
	[[ -x "$b" ]] || die "missing executable $b (redeploy from GitHub Actions)"
done
[[ -f "$MANIFEST" ]] || die "missing ${MANIFEST}"
command -v python3 >/dev/null 2>&1 || die "python3 required to scan shards"

rm -rf "$WORKDIR"
mkdir -p "$WORKDIR"

KNOWN_IDS="${WORKDIR}/known-event-ids.txt"
CANDIDATES="${WORKDIR}/candidates.txt"
MISSING="${WORKDIR}/missing-ids.txt"
SINCE_FILE="${WORKDIR}/since.txt"
SHARD_LIST="${WORKDIR}/existing-shards.txt"

log "scanning ${MANIFEST} for earliest match date + known event_ids ..."
python3 - "$MANIFEST" "$DATA" "$KNOWN_IDS" "$SINCE_FILE" "$SHARD_LIST" <<'PY'
import json
import sys
from pathlib import Path

manifest_path, data_dir, known_path, since_path, shard_list_path = sys.argv[1:6]
data = Path(data_dir)
ids = set()
earliest = None
shards = []

def parse_manifest(path: Path):
    out = []
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        entry = line.split("#", 1)[0].strip()
        if not entry:
            continue
        out.append(entry)
    return out

for entry in parse_manifest(Path(manifest_path)):
    p = Path(entry)
    if not p.is_absolute():
        p = data / entry
    if not p.is_file():
        print(f"missing shard listed in manifest: {entry} → {p}", file=sys.stderr)
        sys.exit(1)
    # Skip a prior run of this backfill shard so we can rewrite it cleanly.
    if p.name == "bcp-matches-backfill-min20.json":
        continue
    shards.append(p.name)
    with p.open(encoding="utf-8") as f:
        rows = json.load(f)
    if not isinstance(rows, list):
        print(f"shard is not a JSON array: {p}", file=sys.stderr)
        sys.exit(1)
    for row in rows:
        if not isinstance(row, dict):
            continue
        eid = (row.get("event_id") or "").strip()
        if eid:
            ids.add(eid)
        d = (row.get("date") or "").strip()
        if not d:
            continue
        day = d[:10]
        if len(day) == 10 and day[4] == "-" and day[7] == "-":
            if earliest is None or day < earliest:
                earliest = day

if earliest is None:
    print("no match dates found in shards", file=sys.stderr)
    sys.exit(1)

Path(known_path).write_text("".join(sorted(i + "\n" for i in ids)), encoding="utf-8")
Path(since_path).write_text(earliest + "\n", encoding="utf-8")
Path(shard_list_path).write_text("".join(s + "\n" for s in shards), encoding="utf-8")
print(f"known_event_ids={len(ids)} earliest={earliest} shards={len(shards)}")
PY

SINCE="$(tr -d '[:space:]' <"$SINCE_FILE")"
[[ -n "$SINCE" ]] || die "failed to determine SINCE from shards"
n_known="$(grep -cve '^[[:space:]]*$' "$KNOWN_IDS" 2>/dev/null || true)"
n_known="${n_known:-0}"
log "SINCE=${SINCE} known_event_ids=${n_known} min-players=${MIN_PLAYERS}"

log "harvesting candidates since ${SINCE} ..."
"$HARVEST_BIN" \
	-since "$SINCE" \
	-min-players "$MIN_PLAYERS" \
	-min-rounds "$MIN_ROUNDS" \
	-max-span-days "$MAX_SPAN_DAYS" \
	-exclude-name "$EXCLUDE_NAME" \
	-min-interval-ms "$HARVEST_MIN_INTERVAL_MS" \
	-out-ids "$CANDIDATES" \
	-out-events-json "${WORKDIR}/events.json"

n_cand="$(grep -cve '^[[:space:]]*$' "$CANDIDATES" 2>/dev/null || true)"
n_cand="${n_cand:-0}"
if [[ "$n_cand" -eq 0 ]]; then
	log "no candidate events since ${SINCE}; nothing to backfill"
	rm -rf "$WORKDIR"
	exit 0
fi

# missing = candidates not in known (order preserved from harvest)
python3 - "$CANDIDATES" "$KNOWN_IDS" "$MISSING" <<'PY'
import sys
from pathlib import Path

cand_path, known_path, missing_path = sys.argv[1:4]
known = {
    line.strip()
    for line in Path(known_path).read_text(encoding="utf-8").splitlines()
    if line.strip()
}
missing = []
seen = set()
for line in Path(cand_path).read_text(encoding="utf-8").splitlines():
    eid = line.strip()
    if not eid or eid in known or eid in seen:
        continue
    seen.add(eid)
    missing.append(eid)
Path(missing_path).write_text("".join(e + "\n" for e in missing), encoding="utf-8")
print(f"missing={len(missing)}")
PY

n_miss="$(grep -cve '^[[:space:]]*$' "$MISSING" 2>/dev/null || true)"
n_miss="${n_miss:-0}"
log "candidates=${n_cand} missing=${n_miss}"

if [[ "$n_miss" -eq 0 ]]; then
	log "all harvested events already present in shards; nothing to export"
	rm -rf "$WORKDIR"
	exit 0
fi

log "exporting ${n_miss} missing events → ${BACKFILL_SHARD_NAME} ..."
set +e
"$EXPORT_BIN" \
	-events-file "$MISSING" \
	-out "$BACKFILL_SHARD" \
	-continue-on-error \
	-sleep-ms "$EXPORT_SLEEP_MS" \
	-min-players "$MIN_PLAYERS" \
	-min-rounds "$MIN_ROUNDS" \
	-max-span-days "$MAX_SPAN_DAYS" \
	-exclude-name "$EXCLUDE_NAME"
export_rc=$?
set -e

if [[ ! -f "$BACKFILL_SHARD" ]]; then
	die "export produced no file (rc=${export_rc})"
fi
if [[ "$export_rc" -ne 0 ]]; then
	log "export exited ${export_rc} but wrote ${BACKFILL_SHARD}; continuing"
fi

log "rewriting ${MANIFEST} (existing shards first, backfill last) ..."
{
	echo "# Generated by backfill-min-players.sh on ${TODAY}; used with: local-elo -matches-manifest bcp-matches.manifest"
	echo "# Shards are merged in listed order; overlaps dedupe (first row wins)."
	cat "$SHARD_LIST"
	echo "$BACKFILL_SHARD_NAME"
} >"$MANIFEST"

log "wrote ${BACKFILL_SHARD} and updated ${MANIFEST}"
rm -rf "$WORKDIR"

if [[ "$SKIP_REFRESH" == "1" ]]; then
	log "SKIP_REFRESH=1 — run: PULL_MATCHES=0 ${REFRESH_SCRIPT}"
	exit 0
fi

[[ -x "$REFRESH_SCRIPT" ]] || die "missing ${REFRESH_SCRIPT}"
log "rebuilding leaderboard (PULL_MATCHES=0) ..."
PULL_MATCHES=0 "$REFRESH_SCRIPT"
log "done"
