// Bot entry: Discord slash commands for Elo leaderboard, player history, and event roster (BCP).
// Run from repo root: DISCORD_BOT_TOKEN=... go run ./discord-bot/cmd/bot -guild YOUR_GUILD_ID
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"fortyk/eloevent/internal/bcp"
	"fortyk/eloevent/internal/elo40k"
	"fortyk/eloevent/pkg/elodata"
)

func main() {
	token := flag.String("token", os.Getenv("DISCORD_BOT_TOKEN"), "bot token (or env DISCORD_BOT_TOKEN)")
	guildID := flag.String("guild", os.Getenv("DISCORD_GUILD_ID"), "optional guild id for instant slash registration")
	matchesPath := flag.String("matches", getenvDefault("ELO_MATCHES_JSON", "bcp-matches.json"), "exported pairings JSON")
	leaderPath := flag.String("leaderboard", getenvDefault("ELO_LEADERBOARD_JSON", "leaderboard.json"), "from local-elo -out-json")
	reloadEvery := flag.Duration("reload", 0, "reload JSON files from disk on this interval (0 = only at startup)")
	flag.Parse()

	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(os.Stderr, "Need DISCORD_BOT_TOKEN (or -token). Example:")
		fmt.Fprintln(os.Stderr, "  DISCORD_BOT_TOKEN=... go run ./discord-bot/cmd/bot -guild YOUR_GUILD_ID \\")
		fmt.Fprintln(os.Stderr, "    -matches bcp-matches.json -leaderboard leaderboard.json")
		fmt.Fprintln(os.Stderr, "See discord-bot/README.md.")
		os.Exit(2)
	}

	bot := &botState{
		matchesPath: *matchesPath,
		leaderPath:  *leaderPath,
		bcpClient:   &bcp.Client{MinInterval: 400 * time.Millisecond},
	}
	if err := bot.reload(); err != nil {
		log.Fatal(err)
	}
	if *reloadEvery > 0 {
		go func() {
			for range time.Tick(*reloadEvery) {
				if err := bot.reload(); err != nil {
					log.Printf("reload: %v", err)
				}
			}
		}()
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

	matchesPath string
	leaderPath  string
	rows        []bcp.MatchFileRow
	lb          *elodata.LeaderboardFile

	bcpClient *bcp.Client
}

func (b *botState) reload() error {
	raw, err := os.ReadFile(b.matchesPath)
	if err != nil {
		return fmt.Errorf("matches: %w", err)
	}
	var rows []bcp.MatchFileRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return fmt.Errorf("matches json: %w", err)
	}
	lb, err := elodata.ReadLeaderboardJSON(b.leaderPath)
	if err != nil {
		return fmt.Errorf("leaderboard: %w", err)
	}
	b.mu.Lock()
	b.rows = rows
	b.lb = lb
	b.mu.Unlock()
	log.Printf("loaded %d match rows, %d leaderboard entries", len(rows), len(lb.Players))
	return nil
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
			name, _ := optString(data.Options, "name")
			contains := optBool(data.Options, "contains")
			last := int64(10)
			if v, ok := optInt(data.Options, "last"); ok {
				last = v
			}
			msg, err = b.handlePlayer(name, contains, int(last))
		case "elo-roster":
			eid, _ := optString(data.Options, "event_id")
			msg, err = b.handleRoster(eid)
		case "elo-leaderboard":
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
	b.mu.RUnlock()
	rep, err := elodata.PlayerLookup(rows, name, contains, last)
	if err != nil {
		return "", err
	}
	if rep == nil {
		return fmt.Sprintf("No games found for %q (try `contains: true`).", name), nil
	}
	var o strings.Builder
	o.WriteString(fmt.Sprintf("**%s** — %d games · **%d–%d–%d** · win %.1f%% · pts %.1f%%\n",
		rep.DisplayName, rep.Wins+rep.Losses+rep.Draws, rep.Wins, rep.Losses, rep.Draws, rep.WinPct, rep.PointsPct))
	if rep.MultiNameWarning {
		o.WriteString("_Substring matched multiple names; stats combined._\n")
	}
	if len(rep.RecentEvents) > 0 {
		o.WriteString("**Recent events**\n```\n")
		for _, ev := range rep.RecentEvents {
			id := strings.TrimSpace(ev.EventID)
			if id == "" {
				id = "—"
			}
			sum := "ΣΔ —"
			if ev.DeltaGames > 0 {
				sum = fmt.Sprintf("ΣΔ %+.1f", ev.TotalDeltaElo)
			}
			o.WriteString(fmt.Sprintf("%s %s %d–%d–%d · %dg · %s · rated Δ %d/%d\n",
				ev.LastPlayed.UTC().Format("2006-01-02"),
				trunc(id, 26),
				ev.Wins, ev.Losses, ev.Draws,
				ev.Games,
				sum,
				ev.DeltaGames,
				ev.Games))
		}
		o.WriteString("```\n")
	}
	o.WriteString("**Games**\n```\n")
	for _, g := range rep.Games {
		de := "  —"
		if g.DeltaElo != nil {
			de = fmt.Sprintf("%+.1f", *g.DeltaElo)
		}
		side := "B"
		if g.AsA {
			side = "A"
		}
		o.WriteString(fmt.Sprintf("%s %c %-28s %6s %7s %s\n",
			g.Time.UTC().Format("2006-01-02"), g.Result, trunc(g.Opponent, 28), side, de, g.EventID))
	}
	o.WriteString("```")
	return o.String(), nil
}

func (b *botState) handleRoster(eventID string) (string, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return "", fmt.Errorf("need event_id")
	}
	b.mu.RLock()
	lb := b.lb
	b.mu.RUnlock()
	if lb == nil {
		return "", fmt.Errorf("leaderboard not loaded")
	}
	roster, err := bcp.FetchRoster(b.bcpClient, eventID)
	if err != nil {
		return "", err
	}
	byRow := lb.RowByKey()
	type line struct {
		name string
		elo  float64
		drop bool
	}
	var lines []line
	for _, p := range roster {
		n := p.FullName()
		if n == "" {
			n = "(unnamed)"
		}
		k := elo40k.PlayerKey(n)
		elo := elo40k.Baseline
		if row, ok := byRow[k]; ok {
			elo = row.Elo
		}
		lines = append(lines, line{name: n, elo: elo, drop: p.Dropped})
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].elo != lines[j].elo {
			return lines[i].elo > lines[j].elo
		}
		return lines[i].name < lines[j].name
	})
	var o strings.Builder
	o.WriteString(fmt.Sprintf("**Roster Elo** · `%s` · %d players (missing from JSON → %.0f)\n```\n", eventID, len(lines), elo40k.Baseline))
	max := 45
	if len(lines) < max {
		max = len(lines)
	}
	for i := 0; i < max; i++ {
		ln := lines[i]
		tag := ""
		if ln.drop {
			tag = " dropped"
		}
		o.WriteString(fmt.Sprintf("%5.1f · %-32s%s\n", ln.elo, trunc(ln.name, 32), tag))
	}
	o.WriteString("```")
	if len(lines) > max {
		o.WriteString(fmt.Sprintf("\n_…+%d more (truncate in Discord)_", len(lines)-max))
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
