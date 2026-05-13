#!/usr/bin/env bash
# One-time setup on the DigitalOcean droplet (run as root).
# From a clone of this repo on the droplet:
#   sudo bash scripts/droplet/install-on-droplet.sh
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT="/opt/eloevent"
mkdir -p "${ROOT}/repo" "${ROOT}/bin" "${ROOT}/data" "${ROOT}/env" "${ROOT}/scripts"

if [[ ! -f "${ROOT}/data/leaderboard.json" ]]; then
  printf '%s\n' '{"as_of":"","players":[]}' >"${ROOT}/data/leaderboard.json"
  echo "Wrote stub ${ROOT}/data/leaderboard.json"
fi

if [[ ! -f "${ROOT}/env/bot.env" ]]; then
  cp "${SCRIPT_DIR}/env/bot.env.example" "${ROOT}/env/bot.env"
  echo "Edit ${ROOT}/env/bot.env with DISCORD_BOT_TOKEN and DISCORD_GUILD_ID, then: chmod 600 ${ROOT}/env/bot.env (CI deploy overwrites this if those values are configured as secrets)"
fi

chmod 700 "${ROOT}/env"
chmod 600 "${ROOT}/env/bot.env" 2>/dev/null || true

cp "${SCRIPT_DIR}/eloevent-discord-bot.service" /etc/systemd/system/eloevent-discord-bot.service
chmod 644 /etc/systemd/system/eloevent-discord-bot.service
cp "${SCRIPT_DIR}/refresh-leaderboard.sh" "${ROOT}/scripts/refresh-leaderboard.sh"
chmod 755 "${ROOT}/scripts/refresh-leaderboard.sh"

systemctl daemon-reload
systemctl enable eloevent-discord-bot.service

echo "Next:"
echo "  1. Fill in ${ROOT}/env/bot.env (chmod 600)"
echo "  2. Put bcp-matches.json at ${ROOT}/data/bcp-matches.json (large file; keep out of git)"
echo "  3. Optional: ${ROOT}/env/refresh.env from env/refresh.env.example (UPDATE_MATCHES_CMD for weekly refresh)"
echo "  4. ${ROOT}/scripts/refresh-leaderboard.sh"
echo "  5. systemctl start eloevent-discord-bot"
echo "  (${ROOT}/repo/ is filled by GitHub Actions deploy rsync after you push to main)"
