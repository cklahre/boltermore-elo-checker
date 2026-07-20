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

if [[ ! -f "${ROOT}/env/refresh.env" ]]; then
  cp "${SCRIPT_DIR}/env/refresh.env.example" "${ROOT}/env/refresh.env"
  chmod 600 "${ROOT}/env/refresh.env"
  echo "Wrote ${ROOT}/env/refresh.env (PULL_MATCHES=1 for weekly BCP harvest)"
fi

chmod 700 "${ROOT}/env"
chmod 600 "${ROOT}/env/bot.env" 2>/dev/null || true

cp "${SCRIPT_DIR}/eloevent-discord-bot.service" /etc/systemd/system/eloevent-discord-bot.service
chmod 644 /etc/systemd/system/eloevent-discord-bot.service
cp "${SCRIPT_DIR}/eloevent-refresh-leaderboard.service" /etc/systemd/system/eloevent-refresh-leaderboard.service
cp "${SCRIPT_DIR}/eloevent-refresh-leaderboard.timer" /etc/systemd/system/eloevent-refresh-leaderboard.timer
chmod 644 /etc/systemd/system/eloevent-refresh-leaderboard.service /etc/systemd/system/eloevent-refresh-leaderboard.timer

cp "${SCRIPT_DIR}/refresh-leaderboard.sh" "${ROOT}/scripts/refresh-leaderboard.sh"
cp "${SCRIPT_DIR}/pull-matches.sh" "${ROOT}/scripts/pull-matches.sh"
cp "${SCRIPT_DIR}/start-eloevent-discord-bot.sh" "${ROOT}/scripts/start-eloevent-discord-bot.sh"
chmod 755 "${ROOT}/scripts/refresh-leaderboard.sh" "${ROOT}/scripts/pull-matches.sh" "${ROOT}/scripts/start-eloevent-discord-bot.sh"

systemctl daemon-reload
systemctl enable eloevent-discord-bot.service
systemctl enable --now eloevent-refresh-leaderboard.timer

echo "Next:"
echo "  1. Fill in ${ROOT}/env/bot.env (chmod 600)"
echo "  2. Seed matches into ${ROOT}/data/ (sync-matches-to-droplet.sh) OR commit shards so CI fills ${ROOT}/repo/ — weekly pull seeds data/ from repo on first run"
echo "  3. Optional tweaks: ${ROOT}/env/refresh.env (SINCE=, EXPORT_SLEEP_MS=, …)"
echo "  4. systemd runs pull+refresh every Monday 20:00 UTC (eloevent-refresh-leaderboard.timer)"
echo "  5. Manual now: PULL_MATCHES=1 ${ROOT}/scripts/refresh-leaderboard.sh"
echo "  6. systemctl start eloevent-discord-bot"
echo "  (${ROOT}/repo/ is filled by GitHub Actions deploy rsync after you push to main)"
