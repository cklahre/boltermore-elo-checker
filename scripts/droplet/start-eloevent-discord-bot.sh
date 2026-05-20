#!/usr/bin/env bash
# Prefer manifest + shard JSON (matches refresh-leaderboard.sh): /opt/eloevent/data/, then CI mirror repo/.
# Monolith bcp-matches.json is intentionally not auto-used — set ELO_MATCHES_JSON in bot.env only if needed.
set -euo pipefail

ROOT=/opt/eloevent
EXE=$ROOT/bin/eloevent-bot
LEADER=$ROOT/data/leaderboard.json

if [[ -n "${ELO_MATCHES_MANIFEST:-}" ]]; then
	exec "$EXE" -matches-manifest "$ELO_MATCHES_MANIFEST" -leaderboard "$LEADER"
fi
if [[ -n "${ELO_MATCHES_JSON:-}" ]]; then
	exec "$EXE" -matches "$ELO_MATCHES_JSON" -leaderboard "$LEADER"
fi

if [[ -f "$ROOT/data/bcp-matches.manifest" ]]; then
	exec "$EXE" -matches-manifest "$ROOT/data/bcp-matches.manifest" -leaderboard "$LEADER"
fi
if [[ -f "$ROOT/repo/bcp-matches.manifest" ]]; then
	exec "$EXE" -matches-manifest "$ROOT/repo/bcp-matches.manifest" -leaderboard "$LEADER"
fi

echo "eloevent-discord-bot: need bcp-matches.manifest (+ shard JSON beside it); under ${ROOT}/data/ or ${ROOT}/repo/, or set ELO_MATCHES_MANIFEST / ELO_MATCHES_JSON in bot.env" >&2
exit 1
