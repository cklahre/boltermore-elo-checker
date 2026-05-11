# Discord bot (Elo + BCP)

This folder stays **separate from the CLI tools** but lives in the **same Go module** (`fortyk/eloevent`) so it can import `internal/bcp` for roster lookups. To move it to **another repo** later, either:

- publish a small library module that exposes `pkg/elodata` + BCP client, or  
- copy `internal/bcp` (and dependencies) into that repo and change imports.

## Data files

1. **Matches** — same JSON as `bcp-export-matches` (`bcp-matches.json`).
2. **Leaderboard cache** — generate with `local-elo` (full pool; no need to recompute in the bot):

```bash
go run ./cmd/local-elo -matches bcp-matches.json -as-of 2026-05-06 -out-json leaderboard.json
```

Restart the bot (or use `-reload 5m`) after updating those files.

## Run

```bash
export DISCORD_BOT_TOKEN="..."
# Strongly recommended while developing (instant slash commands):
export DISCORD_GUILD_ID="your_server_snowflake_id"

go run ./discord-bot/cmd/bot \
  -guild "$DISCORD_GUILD_ID" \
  -matches bcp-matches.json \
  -leaderboard leaderboard.json
```

Optional env (defaults shown):

- `ELO_MATCHES_JSON` → `bcp-matches.json`
- `ELO_LEADERBOARD_JSON` → `leaderboard.json`

Flags override env.

## Slash commands

| Command | Purpose |
|--------|---------|
| `/elo-player` | Name, optional `contains`, optional `last` (default 10 recent games + ΔElo) |
| `/elo-leaderboard` | Optional `top` (default 15) from `leaderboard.json` |
| `/elo-roster` | BCP `event_id` — pulls roster from Best Coast, matches names to your cached Elo |

Roster rows **not** in `leaderboard.json` are shown at baseline **1500** (no games in your export).

## Discord app setup (short)

1. [Discord Developer Portal](https://discord.com/developers/applications) → New Application → Bot → reset token → enable **Privileged Gateway Intent** only if you need presence/members (this bot does not).
2. OAuth2 → URL Generator: scopes **`bot`** + **`applications.commands`**, permission **Send Messages** (and **Use Slash Commands**).
3. Invite the URL, then run the bot with `-guild` so commands appear immediately on your server.

## Shared library

Player stats / deltas are implemented in **`pkg/elodata`** (`PlayerLookup`, `WriteLeaderboardJSON`, `ReadLeaderboardJSON`) so the CLI `player-history` and this bot stay aligned.
