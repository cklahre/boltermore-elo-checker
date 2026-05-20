#!/usr/bin/env bash
# Copy bcp-matches.manifest + shards from your repo → droplet /opt/eloevent/data/, then run
# /opt/eloevent/scripts/refresh-leaderboard.sh (same precedence as systemd / CI layout).
#
# Default manifest: repo-root bcp-matches.manifest when MATCHES_MANIFEST is unset:
#   DO_HOST=x.x.x.x ./scripts/droplet/sync-matches-to-droplet.sh
# Explicit manifest path:
#   MATCHES_MANIFEST=./scripts/droplet/other.manifest DO_HOST=x.x.x.x ./scripts/droplet/sync-matches-to-droplet.sh
#
# Optional emergency monolith ONLY when explicitly requested (drops matching manifest upload pattern):
#   MATCHES_JSON=/path/to/one-big-file.json SYNC_MONOLITH=1 DO_HOST=... ./scripts/droplet/sync-matches-to-droplet.sh
#
# Optional:
#   DO_SSH_USER=root SSH_IDENTITY=~/.ssh/id_do_personal DO_HOST=x.x.x.x ...
set -euo pipefail

: "${DO_HOST:?set DO_HOST to your droplet IP or hostname}"
U="${DO_SSH_USER:-root}"
REMOTE_DATA="/opt/eloevent/data"

ssh_cmd=(ssh)
rsync_ssh_e=(ssh)
if [[ -n "${SSH_IDENTITY:-}" ]]; then
	IID="${SSH_IDENTITY/#\~/$HOME}"
	ssh_cmd+=( -i "$IID" -o IdentitiesOnly=yes )
	rsync_ssh_e+=( -i "$IID" -o IdentitiesOnly=yes )
fi

rsync_rsh() {
	printf '%q' "${rsync_ssh_e[0]}"
	((${#rsync_ssh_e[@]} > 1)) && printf ' %q' "${rsync_ssh_e[@]:1}"
}

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/../.." && pwd)

"${ssh_cmd[@]}" "${U}@${DO_HOST}" "mkdir -p ${REMOTE_DATA}"

upload_manifest_shards() {
	local man="$1"
	[[ -f "$man" ]] || {
		echo "MATCHES_MANIFEST not found: $man" >&2
		exit 1
	}
	local man_abs man_dir raw trimmed entry local_shard bn
	man_abs="$(cd "$(dirname "$man")" && pwd)/$(basename "$man")"
	man_dir="$(dirname "$man_abs")"

	echo "Uploading manifest → ${U}@${DO_HOST}:${REMOTE_DATA}/bcp-matches.manifest ..."
	rsync -avP -e "$(rsync_rsh)" "$man_abs" "${U}@${DO_HOST}:${REMOTE_DATA}/bcp-matches.manifest"

	while IFS= read -r raw || [[ -n "${raw:-}" ]]; do
		raw="${raw//$'\r'/}"
		trimmed="${raw#"${raw%%[![:space:]]*}"}"
		trimmed="${trimmed%"${trimmed##*[![:space:]]}"}"
		[[ -z "$trimmed" ]] && continue
		[[ "${trimmed:0:1}" == '#' ]] && continue
		entry="${trimmed%%#*}"
		entry="${entry%"${entry##*[![:space:]]}"}"
		if [[ "$entry" = /* ]]; then
			local_shard="$entry"
		else
			local_shard="${man_dir}/${entry}"
		fi
		[[ -f "$local_shard" ]] || {
			echo "matches shard not found (from manifest ${man_abs}:): $entry → $local_shard" >&2
			exit 1
		}
		bn="$(basename "$entry")"
		echo "Uploading shard ${bn} ..."
		rsync -avP -e "$(rsync_rsh)" "$local_shard" "${U}@${DO_HOST}:${REMOTE_DATA}/${bn}"
	done <"$man_abs"
}

upload_single_matches() {
	local src="${MATCHES_JSON:?with SYNC_MONOLITH=1 also set MATCHES_JSON to the JSON file path}"
	if [[ ! -f "$src" ]]; then
		echo "matches file not found: $src" >&2
		exit 1
	fi
	echo "Removing ${REMOTE_DATA}/bcp-matches.manifest on server (monolith overrides manifest pool) ..."
	"${ssh_cmd[@]}" "${U}@${DO_HOST}" "rm -f ${REMOTE_DATA}/bcp-matches.manifest"
	echo "Uploading monolith → ${U}@${DO_HOST}:${REMOTE_DATA}/bcp-matches.json ..."
	rsync -avP -e "$(rsync_rsh)" "$src" "${U}@${DO_HOST}:${REMOTE_DATA}/bcp-matches.json"
}

if [[ "${SYNC_MONOLITH:-}" == "1" ]]; then
	upload_single_matches
elif [[ -n "${MATCHES_MANIFEST:-}" ]]; then
	upload_manifest_shards "${MATCHES_MANIFEST}"
elif [[ -f "${REPO_ROOT}/bcp-matches.manifest" ]]; then
	upload_manifest_shards "${REPO_ROOT}/bcp-matches.manifest"
else
	echo "No MATCHES_MANIFEST and no ${REPO_ROOT}/bcp-matches.manifest." >&2
	echo "Use dated shards listed in your manifest at repo root, or MATCHES_MANIFEST=/path/to/your.manifest" >&2
	echo "Emergency only: SYNC_MONOLITH=1 MATCHES_JSON=/path/to/file.json ./scripts/droplet/sync-matches-to-droplet.sh ..." >&2
	exit 1
fi

echo "Running refresh-leaderboard on server ..."
"${ssh_cmd[@]}" "${U}@${DO_HOST}" bash /opt/eloevent/scripts/refresh-leaderboard.sh

echo "Done."
if [[ -n "${SSH_IDENTITY:-}" ]]; then
	IID="${SSH_IDENTITY/#\~/$HOME}"
	echo "Confirm: SSH_IDENTITY=\"$IID\" ssh ${U}@${DO_HOST} 'systemctl status eloevent-discord-bot --no-pager'"
else
	echo "Confirm: ssh ${U}@${DO_HOST} 'systemctl status eloevent-discord-bot --no-pager'"
fi
