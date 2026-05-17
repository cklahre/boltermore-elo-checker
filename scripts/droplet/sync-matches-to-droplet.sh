#!/usr/bin/env bash
# Copy bcp-matches.json from your machine → droplet, rebuild leaderboard.json, restart the bot.
# Prerequisites: SSH access (same user/key you use for the droplet), binaries already deployed by CI.
#
# Usage (from repo root):
#   DO_HOST=x.x.x.x ./scripts/droplet/sync-matches-to-droplet.sh
#
# Optional:
#   DO_SSH_USER=root MATCHES_JSON=/path/to/bcp-matches.json DO_HOST=x.x.x.x ./scripts/droplet/sync-matches-to-droplet.sh
#   SSH_IDENTITY=~/.ssh/id_do_personal DO_HOST=x.x.x.x ./scripts/droplet/sync-matches-to-droplet.sh
set -euo pipefail

: "${DO_HOST:?set DO_HOST to your droplet IP or hostname}"
U="${DO_SSH_USER:-root}"

ssh_cmd=(ssh)
rsync_ssh_e=(ssh)
if [[ -n "${SSH_IDENTITY:-}" ]]; then
  IID="${SSH_IDENTITY/#\~/$HOME}"
  ssh_cmd+=( -i "$IID" -o IdentitiesOnly=yes )
  rsync_ssh_e+=( -i "$IID" -o IdentitiesOnly=yes )
fi

rsync_rsh() {
  printf '%q' "${rsync_ssh_e[0]}"
  (( ${#rsync_ssh_e[@]} > 1 )) && printf ' %q' "${rsync_ssh_e[@]:1}"
}
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "${SCRIPT_DIR}/../.." && pwd)
SRC="${MATCHES_JSON:-${REPO_ROOT}/bcp-matches.json}"

if [[ ! -f "$SRC" ]]; then
  echo "matches file not found: $SRC" >&2
  exit 1
fi

echo "Uploading matches → ${U}@${DO_HOST}:/opt/eloevent/data/bcp-matches.json ..."
rsync -avP -e "$(rsync_rsh)" "$SRC" "${U}@${DO_HOST}:/opt/eloevent/data/bcp-matches.json"

echo "Generating leaderboard.json and restarting bot ..."
"${ssh_cmd[@]}" "${U}@${DO_HOST}" bash <<'REMOTE'
set -euo pipefail
mkdir -p /opt/eloevent/data
/opt/eloevent/bin/local-elo \
  -matches /opt/eloevent/data/bcp-matches.json \
  -out-json /opt/eloevent/data/leaderboard.json
systemctl restart eloevent-discord-bot
REMOTE

echo "Done."
if [[ -n "${SSH_IDENTITY:-}" ]]; then
  IID="${SSH_IDENTITY/#\~/$HOME}"
  echo "Confirm: SSH_IDENTITY=\"$IID\" ssh ${U}@${DO_HOST} 'systemctl status eloevent-discord-bot --no-pager'"
else
  echo "Confirm: ssh ${U}@${DO_HOST} 'systemctl status eloevent-discord-bot --no-pager'"
fi
