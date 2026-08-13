// Bot entry: Discord slash commands for Elo leaderboard, player history, and event roster (BCP).
// Run from repo root: DISCORD_BOT_TOKEN=... go run ./discord-bot/cmd/bot -guild YOUR_GUILD_ID
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
	"fortyk/eloevent/pkg/elodata"
)

type matchPathsFlag []string

func (m *matchPathsFlag) String() string { return strings.Join([]string(*m), ", ") }
func (m *matchPathsFlag) Set(v string) error {
	*m = append(*m, strings.TrimSpace(v))
	return nil
}

func main() {
	token := flag.String("token", os.Getenv("DISCORD_BOT_TOKEN"), "bot token (or env DISCORD_BOT_TOKEN)")
	guildID := flag.String("guild", os.Getenv("DISCORD_GUILD_ID"), "optional guild id for instant slash registration")
	matchesManifest := flag.String("matches-manifest", getenvDefault("ELO_MATCHES_MANIFEST", ""), "when non-empty, load ONLY this manifest (see refresh-leaderboard / local-elo); systemd/bot.env: omit -matches for same pool")
	var matchesShards matchPathsFlag
	flag.Var(&matchesShards, "matches", "exported pairings JSON shard (repeat for multi-file)")
	leaderPath := flag.String("leaderboard", getenvDefault("ELO_LEADERBOARD_JSON", "leaderboard.json"), "from local-elo -out-json")
	reloadEvery := flag.Duration("reload", 0, "reload JSON from disk on this interval after connect (0 = startup + slash-triggered reload)")
	announceChan := flag.String("announce-leaderboard-channel", getenvDefault("ELO_LEADERBOARD_ANNOUNCE_CHANNEL", ""), "channel ID or #elo-hell: post when leaderboard as_of changes (-guild required to resolve names)")
	flag.Parse()

	sources, err := bcp.ResolveMatchShardPaths(*matchesManifest, matchesShards, getenvDefault("ELO_MATCHES_JSON", ""))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(os.Stderr, "Need DISCORD_BOT_TOKEN (or -token). Example:")
		fmt.Fprintln(os.Stderr, "  DISCORD_BOT_TOKEN=... go run ./discord-bot/cmd/bot -guild YOUR_GUILD_ID \\")
		fmt.Fprintln(os.Stderr, "    -matches-manifest bcp-matches.manifest -leaderboard leaderboard.json")
		fmt.Fprintln(os.Stderr, "See discord-bot/README.md.")
		os.Exit(2)
	}

	bot := &botState{
		matchShardPaths:     sources,
		leaderPath:          *leaderPath,
		announceGuildID:     strings.TrimSpace(*guildID),
		announceChannelSpec: strings.TrimSpace(*announceChan),
		bcpClient: &bcp.Client{
			MinInterval: 400 * time.Millisecond,
			BearerToken: strings.TrimSpace(os.Getenv("BCP_BEARER_TOKEN")),
		},
	}
	if err := bot.reload(nil); err != nil {
		log.Fatal(err)
	}

	s, err := discordgo.New("Bot " + *token)
	if err != nil {
		log.Fatal(err)
	}
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("logged in as %s", r.User.Username)
		if err := registerSlashCommands(s, *guildID); err != nil {
			log.Printf("slash commands: %v", err)
		}
	})
	s.AddHandler(bot.onInteraction())

	if err := s.Open(); err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	if *reloadEvery > 0 {
		go func(session *discordgo.Session) {
			for range time.Tick(*reloadEvery) {
				if err := bot.reload(session); err != nil {
					log.Printf("reload: %v", err)
				}
			}
		}(s)
	}
	log.Println("bot running; Ctrl+C to exit")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}

func getenvDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

type botState struct {
	mu sync.RWMutex

	matchShardPaths []string
	leaderPath      string
	rows            []bcp.MatchFileRow
	lb              *elodata.LeaderboardFile
	diskSig         string // stat fingerprint of leaderboard + shards; stale → reloadFromDiskIfNeeded reloads

	announceGuildID     string
	announceChannelSpec string
	announceMu          sync.Mutex
	resolvedAnnounceID  string

	bcpClient *bcp.Client
}

func (b *botState) reload(announceSession *discordgo.Session) error {
	rows, err := bcp.MergeMatchFiles(b.matchShardPaths, true)
	if err != nil {
		return fmt.Errorf("matches merge: %w", err)
	}
	lb, err := elodata.ReadLeaderboardJSON(b.leaderPath)
	if err != nil {
		return fmt.Errorf("leaderboard: %w", err)
	}
	fp, ferr := botDataFingerprint(b.matchShardPaths, b.leaderPath)
	if ferr != nil {
		return ferr
	}
	newAsOf := strings.TrimSpace(lb.AsOfRFC3339)
	nPlayers := len(lb.Players)

	b.mu.Lock()
	b.rows = rows
	b.lb = lb
	b.diskSig = fp
	b.mu.Unlock()
	log.Printf("loaded %d match rows (%d shards), %d leaderboard entries", len(rows), len(b.matchShardPaths), nPlayers)

	if announceSession != nil {
		go b.maybeAnnounceLeaderboardUpdate(announceSession, newAsOf, nPlayers)
	} else if strings.TrimSpace(b.announceChannelSpec) != "" {
		b.seedLeaderboardAnnounceCursor(newAsOf)
	}
	return nil
}

// seedLeaderboardAnnounceCursor records the current leaderboard as_of after startup reload without
// posting Discord (needed so the next refresh with a newer as_of triggers an announcement).
func (b *botState) seedLeaderboardAnnounceCursor(newAsOf string) {
	newAsOf = strings.TrimSpace(newAsOf)
	if newAsOf == "" {
		return
	}
	prev, _, err := readLeaderboardAnnounceStamp(b.leaderPath)
	if err != nil || strings.TrimSpace(prev) != "" {
		return
	}
	if err := writeLeaderboardAnnounceStamp(b.leaderPath, newAsOf); err != nil {
		log.Printf("leaderboard announce stamp: seed write: %v", err)
		return
	}
	log.Printf("leaderboard announce stamp: seeded as_of=%s (no Discord post)", newAsOf)
}

func leaderboardAnnounceStampPath(leaderJSONPath string) string {
	dir := filepath.Dir(leaderJSONPath)
	return filepath.Join(dir, ".leaderboard-announce-as_of")
}

func readLeaderboardAnnounceStamp(leaderJSONPath string) (asOfRFC string, emptyFile bool, err error) {
	p := leaderboardAnnounceStampPath(leaderJSONPath)
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", true, nil
		}
		return "", false, err
	}
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return "", true, nil
	}
	return line, false, nil
}

func writeLeaderboardAnnounceStamp(leaderJSONPath, asOfRFC string) error {
	p := leaderboardAnnounceStampPath(leaderJSONPath)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(p); err == nil {
		mode = st.Mode()
	}
	return os.WriteFile(p, []byte(strings.TrimSpace(asOfRFC)+"\n"), mode)
}

func (b *botState) maybeAnnounceLeaderboardUpdate(sess *discordgo.Session, newAsOf string, nPlayers int) {
	if sess == nil {
		return
	}
	newAsOf = strings.TrimSpace(newAsOf)
	if strings.TrimSpace(b.announceChannelSpec) == "" || newAsOf == "" {
		return
	}
	prev, blankStamp, err := readLeaderboardAnnounceStamp(b.leaderPath)
	if err != nil {
		log.Printf("leaderboard announce stamp: read %v", err)
		return
	}
	if blankStamp {
		if err := writeLeaderboardAnnounceStamp(b.leaderPath, newAsOf); err != nil {
			log.Printf("leaderboard announce stamp: seed: %v", err)
			return
		}
		log.Printf("leaderboard announce stamp: first run; recorded as_of=%s (no post)", newAsOf)
		return
	}
	prev = strings.TrimSpace(prev)
	if prev == newAsOf {
		return
	}

	chID, err := b.resolveAnnounceChannelID(sess)
	if err != nil || chID == "" {
		log.Printf("leaderboard announce: skip (%v)", err)
		return
	}
	short := leaderboardAsOfShort(newAsOf)
	body := fmt.Sprintf(
		"📊 Leaderboard refreshed (`as_of` **%s**). **%d** players — try `/elo-leaderboard` or `/elo-player`.",
		short, nPlayers,
	)
	if _, err := sess.ChannelMessageSend(chID, body); err != nil {
		log.Printf("leaderboard announce send: %v", err)
		return
	}
	if err := writeLeaderboardAnnounceStamp(b.leaderPath, newAsOf); err != nil {
		log.Printf("leaderboard announce stamp: write after post: %v", err)
	}
}

func discordSnowflakeString(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 17 || len(s) > 22 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (b *botState) resolveAnnounceChannelID(s *discordgo.Session) (string, error) {
	spec := strings.TrimSpace(strings.TrimPrefix(b.announceChannelSpec, "#"))
	if spec == "" {
		return "", fmt.Errorf("no announce channel configured")
	}
	b.announceMu.Lock()
	defer b.announceMu.Unlock()
	if b.resolvedAnnounceID != "" {
		return b.resolvedAnnounceID, nil
	}
	if discordSnowflakeString(spec) {
		b.resolvedAnnounceID = spec
		log.Printf("leaderboard announcements → channel id %s", spec)
		return b.resolvedAnnounceID, nil
	}
	gid := strings.TrimSpace(b.announceGuildID)
	if gid == "" {
		return "", fmt.Errorf("need DISCORD_GUILD_ID (or -guild) to resolve channel %q by name", spec)
	}
	chs, err := s.GuildChannels(gid)
	if err != nil {
		return "", err
	}
	want := strings.ToLower(spec)
	for _, ch := range chs {
		if ch.Type != discordgo.ChannelTypeGuildText && ch.Type != discordgo.ChannelTypeGuildNews {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(ch.Name))
		if name != want {
			continue
		}
		b.resolvedAnnounceID = ch.ID
		log.Printf("leaderboard announcements → #%s (%s)", ch.Name, ch.ID)
		return b.resolvedAnnounceID, nil
	}
	return "", fmt.Errorf("no text/news channel named %q in guild %s", spec, gid)
}

// reloadFromDiskIfNeeded reloads merged matches + leaderboard when any shard or leaderboard.json changes on disk.
// Without this (and with -reload 0), Discord keeps the snapshot from process start until restart.
func (b *botState) reloadFromDiskIfNeeded(sess *discordgo.Session) error {
	fp, err := botDataFingerprint(b.matchShardPaths, b.leaderPath)
	if err != nil {
		return err
	}
	b.mu.RLock()
	same := fp == b.diskSig && b.diskSig != ""
	b.mu.RUnlock()
	if same {
		return nil
	}
	log.Printf("matches/leaderboard changed on disk; reloading")
	return b.reload(sess)
}

func botDataFingerprint(shardPaths []string, leaderPath string) (string, error) {
	var b strings.Builder
	for _, p := range shardPaths {
		sig, err := fileStatFingerprint(p)
		if err != nil {
			return "", fmt.Errorf("stat shard %s: %w", p, err)
		}
		b.WriteString(sig)
		b.WriteByte('|')
	}
	ls, err := fileStatFingerprint(leaderPath)
	if err != nil {
		return "", fmt.Errorf("stat leaderboard %s: %w", leaderPath, err)
	}
	b.WriteString(ls)
	return b.String(), nil
}

func fileStatFingerprint(path string) (string, error) {
	path = filepath.Clean(path)
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s;%d;%d;%d", path, st.ModTime().UnixNano(), st.Size(), st.Mode()), nil
}

func (b *botState) onInteraction() func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		data := i.ApplicationCommandData()
		var msg string
		var err error
		switch data.Name {
		case "elo-player":
			if rerr := b.reloadFromDiskIfNeeded(s); rerr != nil {
				msg, err = "", rerr
				break
			}
			name, _ := optString(data.Options, "name")
			contains := optBool(data.Options, "contains")
			last := int64(10)
			if v, ok := optInt(data.Options, "last"); ok {
				last = v
			}
			msg, err = b.handlePlayer(name, contains, int(last))
		case "elo-roster":
			if rerr := b.reloadFromDiskIfNeeded(s); rerr != nil {
				em := "Error: " + rerr.Error()
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{Content: em},
				})
				return
			}
			eid, _ := optString(data.Options, "event_id")
			eid = bcp.ParseEventID(eid)
			if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			}); err != nil {
				return
			}
			chunks, rerr := b.rosterDiscordMessages(eid)
			if rerr != nil {
				em := "Error: " + rerr.Error()
				_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &em})
				return
			}
			if len(chunks) == 0 {
				em := "Error: empty roster reply"
				_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &em})
				return
			}
			c0 := chunks[0]
			if _, ferr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &c0}); ferr != nil {
				log.Printf("elo-roster edit: %v", ferr)
				return
			}
			for j := 1; j < len(chunks); j++ {
				cj := chunks[j]
				if _, ferr := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: cj}); ferr != nil {
					log.Printf("elo-roster follow-up %d: %v", j, ferr)
				}
			}
			return
		case "elo-factions":
			eid, _ := optString(data.Options, "event_id")
			eid = bcp.ParseEventID(eid)
			if strings.TrimSpace(b.bcpClient.BearerToken) != "" {
				err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
				})
				if err != nil {
					return
				}
				msg, err = b.handleFactionBreakdown(eid)
				if err != nil {
					msg = "Error: " + err.Error()
				}
				if len(msg) > 1900 {
					msg = msg[:1900] + "…"
				}
				_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg})
				return
			}
			msg, err = b.handleFactionBreakdown(eid)
		case "elo-leaderboard":
			if rerr := b.reloadFromDiskIfNeeded(s); rerr != nil {
				msg, err = "", rerr
				break
			}
			top := int64(15)
			if v, ok := optInt(data.Options, "top"); ok {
				top = v
			}
			msg, err = b.handleLeaderboard(int(top))
		default:
			return
		}
		if err != nil {
			msg = "Error: " + err.Error()
		}
		if len(msg) > 1900 {
			msg = msg[:1900] + "…"
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: msg},
		})
	}
}

func (b *botState) handlePlayer(name string, contains bool, last int) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("need a name")
	}
	b.mu.RLock()
	rows := b.rows
	lb := b.lb
	b.mu.RUnlock()
	rep, err := elodata.PlayerLookup(rows, name, contains, last)
	if err != nil {
		return "", err
	}
	if rep == nil {
		return fmt.Sprintf("No games found for %q (try `contains: true`).", name), nil
	}
	var o strings.Builder
	eloLead := ""
	if lb != nil {
		rrow, ok := lb.LookupPlayerRow(rep.DisplayName)
		asOf := leaderboardAsOfShort(lb.AsOfRFC3339)
		if ok {
			eloLead = fmt.Sprintf("**%.1f Elo** (#%d) · _snapshot %s_", rrow.Elo, rrow.Rank, asOf)
		} else {
			eloLead = fmt.Sprintf("**%.0f** baseline _(not on leaderboard snapshot · %s)_", elo40k.Baseline, asOf)
		}
	} else {
		eloLead = "_leaderboard snapshot not loaded_"
	}
	o.WriteString(fmt.Sprintf("**%s** — %s\n", rep.DisplayName, eloLead))
	o.WriteString(fmt.Sprintf("**Record** — %d games · **%d–%d–%d** · win %.1f%% · pts %.1f%%\n",
		rep.Wins+rep.Losses+rep.Draws, rep.Wins, rep.Losses, rep.Draws, rep.WinPct, rep.PointsPct))
	if rep.MultiNameWarning {
		dn := strings.ReplaceAll(strings.TrimSpace(trunc(rep.DisplayName, 120)), "`", "'")
		o.WriteString("_Substring matched multiple pairing names — combined record._\n")
		o.WriteString(fmt.Sprintf("_**Elo** uses leaderboard key `%s` (first pairing name encountered)._\n", dn))
	}
	if len(rep.RecentEvents) > 0 {
		o.WriteString("**Recent events**\n")
		for _, ev := range rep.RecentEvents {
			sum := "ΣΔ —"
			if ev.DeltaGames > 0 {
				sum = fmt.Sprintf("ΣΔ %+.1f", ev.TotalDeltaElo)
			}
			o.WriteString(fmt.Sprintf("• %s · %s · **%d–%d–%d** · %dg · %s · rated Δ %d/%d\n",
				ev.LastPlayed.UTC().Format("2006-01-02"),
				bcp.EventMarkdownLink(ev.EventID),
				ev.Wins, ev.Losses, ev.Draws,
				ev.Games,
				sum,
				ev.DeltaGames,
				ev.Games))
		}
	}
	o.WriteString("**Games**\n")
	for _, g := range rep.Games {
		de := "—"
		if g.DeltaElo != nil {
			de = fmt.Sprintf("%+.1f", *g.DeltaElo)
		}
		side := "B"
		if g.AsA {
			side = "A"
		}
		o.WriteString(fmt.Sprintf("• %s **%c** vs %s · %s · Δ %s · %s\n",
			g.Time.UTC().Format("2006-01-02"),
			g.Result,
			trunc(g.Opponent, 28),
			side,
			de,
			bcp.EventMarkdownLink(g.EventID)))
	}
	return o.String(), nil
}

// discordPayloadBytes is a conservative cap so edit + follow-up bodies stay under Discord’s 2000-char limit (length in bytes).
const discordPayloadBytes = 1850

// rosterDiscordMessages returns one or more messages (markdown + monospace table) covering the full BCP roster.
func (b *botState) rosterDiscordMessages(eventID string) ([]string, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil, fmt.Errorf("need event_id")
	}
	b.mu.RLock()
	lb := b.lb
	b.mu.RUnlock()
	if lb == nil {
		return nil, fmt.Errorf("leaderboard not loaded")
	}

	ev, err := bcp.FetchEvent(b.bcpClient, eventID)
	if err != nil {
		return nil, err
	}
	head := eventID
	if ev != nil && strings.TrimSpace(ev.Name) != "" {
		head = strings.TrimSpace(ev.Name)
	}

	roster, err := bcp.FetchRoster(b.bcpClient, eventID)
	if err != nil {
		return nil, err
	}
	if len(roster) == 0 {
		hdr := fmt.Sprintf("**Event roster · Elo** · %s · %s\n"+"_BCP returned no players for this event._", head, bcp.EventMarkdownLink(eventID))
		return []string{hdr}, nil
	}

	type line struct {
		name string
		elo  float64
		drop bool
	}
	var rows []line

	for _, p := range roster {
		n := p.FullName()
		if n == "" {
			n = "(unnamed)"
		}
		el := elo40k.Baseline
		if n != "(unnamed)" {
			if row, ok := lb.LookupPlayerRow(n); ok {
				el = row.Elo
			}
		}
		rows = append(rows, line{name: n, elo: el, drop: p.Dropped})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].elo != rows[j].elo {
			return rows[i].elo > rows[j].elo
		}
		return rows[i].name < rows[j].name
	})

	asOfDisp := leaderboardAsOfShort(lb.AsOfRFC3339)
	colHeader := fmt.Sprintf("%-6s %8s %-36s %-4s\n", "Rank", "Elo", "Player", "St")
	sepLine := strings.Repeat("-", len([]rune(colHeader))) + "\n"
	tableHead := colHeader + sepLine

	tableLines := make([]string, len(rows))
	ranks := make([]int, len(rows))
	for i := range rows {
		r := rows[i]
		rank := i + 1
		ranks[i] = rank
		st := ""
		if r.drop {
			st = "OUT"
		}
		tableLines[i] = fmt.Sprintf("%-6d %8.1f %-36s %-4s", rank, r.elo, trunc(r.name, 36), st)
	}

	hdrPart1 := fmt.Sprintf(
		"**Event roster · Elo** · %s · %s\n"+
			"**Players:** %d · Leaderboard **`as_of`** `%s`\n"+
			"_Sorted by current Elo (high → low). Missing from leaderboard JSON → **`%.0f`**._\n"+
			"_`OUT` = dropped from event._\n",
		head, bcp.EventMarkdownLink(eventID), len(rows), asOfDisp, elo40k.Baseline,
	)

	return packRosterTableChunks(hdrPart1, eventID, head, len(rows), tableHead, tableLines, ranks), nil
}

func leaderboardAsOfShort(asOfRFC string) string {
	asOfRFC = strings.TrimSpace(asOfRFC)
	t, err := time.Parse(time.RFC3339, asOfRFC)
	if err != nil || t.IsZero() {
		if len(asOfRFC) > 32 {
			return asOfRFC[:32] + "…"
		}
		return asOfRFC
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// packRosterTableChunks greedily splits table rows into Discord-sized messages using newline boundaries inside a code block.
func packRosterTableChunks(
	hdrPart1, eventID, head string,
	nPlayers int,
	tableHead string,
	rowLines []string,
	ranks []int,
) []string {
	openFence := "```\n"
	closeFence := "```"

	part := 1
	idx := 0
	var chunks []string
	for idx < len(rowLines) {
		startRank := ranks[idx]

		var linesInChunk []string
		for idx < len(rowLines) {
			proposedHi := ranks[idx]
			hdr := hdrPart1
			if part > 1 {
				hdr = rosterContinuationHdr(part, head, eventID, nPlayers, startRank, proposedHi)
			}
			body := rosterTableChunkBody(tableHead, linesInChunk, rowLines[idx])
			msgLen := len(hdr) + len(openFence) + len(body) + len(closeFence)
			if msgLen <= discordPayloadBytes {
				linesInChunk = append(linesInChunk, rowLines[idx])
				idx++
				continue
			}
			if len(linesInChunk) > 0 {
				break
			}
			room := discordPayloadBytes - len(hdr) - len(openFence) - len(closeFence) - len(tableHead) - 1
			if room < 48 {
				room = 48
			}
			short := truncateStringToUTF8(rowLines[idx]+"\n", room)
			if short == "" {
				short = "…\n"
			}
			linesInChunk = append(linesInChunk, strings.TrimSuffix(short, "\n"))
			idx++
			break
		}

		if len(linesInChunk) == 0 {
			break
		}
		endRank := ranks[idx-1]
		finalHdr := hdrPart1
		if part > 1 {
			finalHdr = rosterContinuationHdr(part, head, eventID, nPlayers, startRank, endRank)
		}
		body := rosterTableChunkBody(tableHead, linesInChunk, "")
		chunks = append(chunks, finalHdr+openFence+body+closeFence)
		part++
	}
	return chunks
}

func rosterContinuationHdr(partNum int, head, eventID string, nPlayers, lo, hi int) string {
	return fmt.Sprintf(
		"**Event roster · Elo** · _continued · part **%d**_ · %s · %s\n"+
			"_Ranks **%d**–**%d** of **%d** players · same ordering (Elo high → low)._\n",
		partNum, head, bcp.EventMarkdownLink(eventID), lo, hi, nPlayers,
	)
}

func rosterTableChunkBody(tableHead string, lines []string, nextLineIfFit string) string {
	var b strings.Builder
	b.WriteString(tableHead)
	for _, ln := range lines {
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	if nextLineIfFit != "" {
		b.WriteString(nextLineIfFit)
		b.WriteByte('\n')
	}
	return b.String()
}

func truncateStringToUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	b := []byte(s)
	if len(b) <= maxBytes {
		return string(b)
	}
	b = b[:maxBytes]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

func writeBreakdownTable(o *strings.Builder, rows []bcp.CountRow, maxRows int) {
	if len(rows) == 0 {
		o.WriteString("(none)\n")
		return
	}
	n := len(rows)
	if n > maxRows {
		n = maxRows
	}
	for i := 0; i < n; i++ {
		r := rows[i]
		o.WriteString(fmt.Sprintf("%4d · %s\n", r.Count, trunc(r.Label, 56)))
	}
	if len(rows) > maxRows {
		o.WriteString(fmt.Sprintf("… +%d more rows\n", len(rows)-maxRows))
	}
}

func (b *botState) handleFactionBreakdown(eventID string) (string, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return "", fmt.Errorf("need event_id")
	}
	ev, err := bcp.FetchEvent(b.bcpClient, eventID)
	if err != nil {
		return "", err
	}
	roster, err := bcp.FetchRoster(b.bcpClient, eventID)
	if err != nil {
		return "", err
	}
	active := bcp.ActiveRoster(roster)
	byArmy := bcp.FactionCounts(active, func(p bcp.RosterPlayer) string { return p.ArmyFactionName() })
	byDisp := bcp.FactionCounts(active, func(p bcp.RosterPlayer) string { return p.DispositionName() })

	head := eventID
	if ev != nil && strings.TrimSpace(ev.Name) != "" {
		head = strings.TrimSpace(ev.Name)
	}
	dropped := len(roster) - len(active)
	var o strings.Builder
	o.WriteString(fmt.Sprintf("**Faction / disposition** · %s · %s\n", head, bcp.EventMarkdownLink(eventID)))
	o.WriteString(fmt.Sprintf(
		"**Players:** %d active",
		len(active),
	))
	if dropped > 0 {
		o.WriteString(fmt.Sprintf(" · %d dropped (ignored)", dropped))
	}
	o.WriteString("\n\n")

	o.WriteString("**By faction**\n```\n")
	writeBreakdownTable(&o, byArmy, 40)
	o.WriteString("```\n\n")

	o.WriteString("**By disposition**\n```\n")
	writeBreakdownTable(&o, byDisp, 40)
	o.WriteString("```\n")

	if strings.TrimSpace(b.bcpClient.BearerToken) != "" {
		ids := bcp.UniqueListIDs(active)
		if len(ids) > 0 {
			det, failed := bcp.ListDetachmentIndex(b.bcpClient, ids)
			tree := bcp.ArmyDetachmentTree(active, det, failed)
			o.WriteString(fmt.Sprintf("\n**Army → detachment** _(lists loaded: %d)_\n\n", len(ids)))
			o.WriteString(bcp.FormatArmyDetachmentTree(tree, true))
			o.WriteString("\n")
		}
	} else {
		o.WriteString("\n_Disposition comes from the BCP roster (`subFaction`). Optional army→detachment detail needs `BCP_BEARER_TOKEN`._")
	}
	return o.String(), nil
}

func (b *botState) handleLeaderboard(top int) (string, error) {
	if top <= 0 || top > 50 {
		top = 15
	}
	b.mu.RLock()
	lb := b.lb
	b.mu.RUnlock()
	if lb == nil {
		return "", fmt.Errorf("leaderboard not loaded")
	}
	var o strings.Builder
	o.WriteString(fmt.Sprintf("**Leaderboard** · as_of %s · top %d\n```\n", lb.AsOfRFC3339, top))
	n := top
	if len(lb.Players) < n {
		n = len(lb.Players)
	}
	for i := 0; i < n; i++ {
		r := lb.Players[i]
		o.WriteString(fmt.Sprintf("%3d · %5.1f · %3dg · %s\n", r.Rank, r.Elo, r.Games, r.Name))
	}
	o.WriteString("```")
	return o.String(), nil
}

func registerSlashCommands(s *discordgo.Session, guildID string) error {
	appID := s.State.User.ID
	cmds := []*discordgo.ApplicationCommand{
		{
			Name:        "elo-player",
			Description: "Player record + recent games (from your matches JSON)",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Player name", Required: true},
				{Type: discordgo.ApplicationCommandOptionBoolean, Name: "contains", Description: "Substring match on name", Required: false},
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "last", Description: "Games to show (default 10)", Required: false},
			},
		},
		{
			Name:        "elo-roster",
			Description: "Fetch BCP roster for an event and show Elo from cached leaderboard",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "event_id", Description: "Best Coast Pairings event id", Required: true},
			},
		},
		{
			Name:        "elo-factions",
			Description: "Faction + disposition charts from a BCP roster (skips dropped)",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "event_id", Description: "Best Coast Pairings event id or URL", Required: true},
			},
		},
		{
			Name:        "elo-leaderboard",
			Description: "Top slice of cached leaderboard.json",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionInteger, Name: "top", Description: "How many rows (default 15)", Required: false},
			},
		},
	}

	if guildID != "" {
		existing, err := s.ApplicationCommands(appID, guildID)
		if err == nil {
			for _, c := range existing {
				_ = s.ApplicationCommandDelete(appID, guildID, c.ID)
			}
		}
		for _, c := range cmds {
			if _, err := s.ApplicationCommandCreate(appID, guildID, c); err != nil {
				return fmt.Errorf("create %s: %w", c.Name, err)
			}
		}
		log.Printf("registered %d guild slash commands in %s", len(cmds), guildID)
		return nil
	}

	existing, err := s.ApplicationCommands(appID, "")
	if err == nil {
		for _, c := range existing {
			_ = s.ApplicationCommandDelete(appID, "", c.ID)
		}
	}
	for _, c := range cmds {
		if _, err := s.ApplicationCommandCreate(appID, "", c); err != nil {
			return fmt.Errorf("create global %s: %w", c.Name, err)
		}
	}
	log.Printf("registered %d global slash commands (may take ~1h to appear)", len(cmds))
	return nil
}

func optString(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (string, bool) {
	for _, o := range opts {
		if o.Name == name {
			return o.StringValue(), true
		}
	}
	return "", false
}

func optBool(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) bool {
	for _, o := range opts {
		if o.Name == name {
			return o.BoolValue()
		}
	}
	return false
}

func optInt(opts []*discordgo.ApplicationCommandInteractionDataOption, name string) (int64, bool) {
	for _, o := range opts {
		if o.Name == name {
			return o.IntValue(), true
		}
	}
	return 0, false
}

func trunc(s string, max int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
