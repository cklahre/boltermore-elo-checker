#!/usr/bin/env bash
# Copy match export JSON from your machine → droplet /opt/eloevent/data/, then run
# /opt/eloevent/scripts/refresh-leaderboard.sh (manifest-first, matches CI droplet logic).
#
# Single file:
#   DO_HOST=x.x.x.x ./scripts/droplet/sync-matches-to-droplet.sh
# Multi-shard (paths in manifest relative to manifest's directory — see bcp-matches.manifest.example):
#   MATCHES_MANIFEST=./scripts/droplet/foo.manifest DO_HOST=x.x.x.x ./scripts/droplet/sync-matches-to-droplet.sh
#
# Optional:
#   DO_SSH_USER=root MATCHES_JSON=/path/to/bcp-matches.json DO_HOST=x.x.x.x ...
#   SSH_IDENTITY=~/.ssh/id_do_personal DO_HOST=x.x.x.x ...
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
	local src="${MATCHES_JSON:-${REPO_ROOT}/bcp-matches.json}"
	if [[ ! -f "$src" ]]; then
		echo "matches file not found: $src (set MATCHES_JSON=...)" >&2
		exit 1
	fi
	echo "Removing stale ${REMOTE_DATA}/bcp-matches.manifest on server (so refresh uses the single upload) ..."
	"${ssh_cmd[@]}" "${U}@${DO_HOST}" "rm -f ${REMOTE_DATA}/bcp-matches.manifest"
	echo "Uploading matches → ${U}@${DO_HOST}:${REMOTE_DATA}/bcp-matches.json ..."
	rsync -avP -e "$(rsync_rsh)" "$src" "${U}@${DO_HOST}:${REMOTE_DATA}/bcp-matches.json"
}

if [[ -n "${MATCHES_MANIFEST:-}" ]]; then
	upload_manifest_shards "${MATCHES_MANIFEST}"
else
	upload_single_matches
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
