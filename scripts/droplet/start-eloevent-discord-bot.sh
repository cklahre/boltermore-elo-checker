#!/usr/bin/env bash
# Resolve match paths the same way as refresh-leaderboard.sh: prefer /opt/eloevent/data/,
# fall back to the CI mirror at /opt/eloevent/repo/ (manifest + shard JSON tracked in git).
# Leaderboard stays under data/ (local-elo output).
# Optional: pin inputs via ELO_MATCHES_MANIFEST or ELO_MATCHES_JSON in bot.env (passed by systemd).
set -euo pipefail

ROOT=/opt/eloevent
EXE=$ROOT/bin/eloevent-bot
LEADER=$ROOT/data/leaderboard.json

if [[ -n "${ELO_MATCHES_MANIFEST:-}" ]]; then
	exec "$EXE" -leaderboard "$LEADER"
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
if [[ -f "$ROOT/data/bcp-matches.json" ]]; then
	exec "$EXE" -matches "$ROOT/data/bcp-matches.json" -leaderboard "$LEADER"
fi
if [[ -f "$ROOT/repo/bcp-matches.json" ]]; then
	exec "$EXE" -matches "$ROOT/repo/bcp-matches.json" -leaderboard "$LEADER"
fi

echo "eloevent-discord-bot: need bcp-matches.manifest (+ shards), or bcp-matches.json, under ${ROOT}/data/ or ${ROOT}/repo/, or set ELO_MATCHES_MANIFEST / ELO_MATCHES_JSON in bot.env" >&2
exit 1
