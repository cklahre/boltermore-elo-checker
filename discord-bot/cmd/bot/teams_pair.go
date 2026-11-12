package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	tpPrefix = "tp:"

	tpPhaseSetup    = "setup"
	tpPhaseNominate = "nominate"
	tpPhasePropose  = "propose"
	tpPhaseAccept   = "accept"
	tpPhaseTables   = "tables"
	tpPhaseDone     = "done"
)

type teamPairPlayer struct {
	ID   string
	Name string
}

type teamPairMatch struct {
	Defender teamPairPlayer
	Attacker teamPairPlayer
	// DefenderTeam is "a" or "b" — whose defender this match features.
	DefenderTeam string
	Table        string
}

type teamPairSession struct {
	mu sync.Mutex

	ID        string
	ChannelID string
	MessageID string
	CreatedAt time.Time
	Size      int
	Phase     string

	CaptainA string
	CaptainB string

	TeamA []teamPairPlayer
	TeamB []teamPairPlayer

	RemainingA map[string]teamPairPlayer
	RemainingB map[string]teamPairPlayer

	TablesLeft []string
	Matches    []teamPairMatch

	// Per-round secret / in-progress state
	DefenderA string
	DefenderB string
	// Attackers proposed by each captain to face the OPPOSING defender.
	ProposedByA []string // team A players offered vs defender B
	ProposedByB []string // team B players offered vs defender A
	AcceptedForA string  // team B player accepted to face defender A
	AcceptedForB string  // team A player accepted to face defender B

	PendingTableMatch int // index into Matches awaiting table pick; -1 if none
}

type teamPairHub struct {
	mu       sync.Mutex
	sessions map[string]*teamPairSession
}

func newTeamPairHub() *teamPairHub {
	return &teamPairHub{sessions: make(map[string]*teamPairSession)}
}

func (h *teamPairHub) put(s *teamPairSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[s.ID] = s
}

func (h *teamPairHub) get(id string) *teamPairSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id]
}

func (h *teamPairHub) delete(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, id)
}

func shortSessionID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func tpCustomID(sessionID, action string) string {
	return tpPrefix + sessionID + ":" + action
}

func parseTpCustomID(customID string) (sessionID, action string, ok bool) {
	if !strings.HasPrefix(customID, tpPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(customID, tpPrefix)
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func displayName(u *discordgo.User) string {
	if u == nil {
		return "Unknown"
	}
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

func intPtr(v int) *int { return &v }

func parseTableNames(raw string, size int) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		out := make([]string, size)
		for i := 0; i < size; i++ {
			out[i] = fmt.Sprintf("Table %d", i+1)
		}
		return out, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("duplicate table name %q", name)
		}
		seen[key] = true
		out = append(out, name)
	}
	if len(out) != size {
		return nil, fmt.Errorf("need exactly %d table names (got %d)", size, len(out))
	}
	return out, nil
}

func (b *botState) handleTeamsPairCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	size := 5
	if v, ok := optInt(data.Options, "size"); ok {
		size = int(v)
	}
	if size < 2 || size > 8 {
		respondEphemeral(s, i, "size must be between 2 and 8")
		return
	}
	capA, okA := optUserResolved(i, "captain_a")
	capB, okB := optUserResolved(i, "captain_b")
	if !okA || !okB || capA == nil || capB == nil {
		respondEphemeral(s, i, "Need both captain_a and captain_b")
		return
	}
	if capA.ID == capB.ID {
		respondEphemeral(s, i, "Captains must be different people")
		return
	}
	tablesRaw, _ := optString(data.Options, "tables")
	tables, err := parseTableNames(tablesRaw, size)
	if err != nil {
		respondEphemeral(s, i, "Tables: "+err.Error())
		return
	}

	sid := shortSessionID()
	sess := &teamPairSession{
		ID:                sid,
		ChannelID:         i.ChannelID,
		CreatedAt:         time.Now().UTC(),
		Size:              size,
		Phase:             tpPhaseSetup,
		CaptainA:          capA.ID,
		CaptainB:          capB.ID,
		RemainingA:        map[string]teamPairPlayer{},
		RemainingB:        map[string]teamPairPlayer{},
		TablesLeft:        tables,
		PendingTableMatch: -1,
	}
	b.pairings.put(sess)

	content := sess.renderBoard()
	components := sess.components()
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: components,
			AllowedMentions: &discordgo.MessageAllowedMentions{
				Users: []string{capA.ID, capB.ID},
			},
		},
	})
	if err != nil {
		log.Printf("teams-pair respond: %v", err)
		b.pairings.delete(sid)
		return
	}
	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		log.Printf("teams-pair fetch response: %v", err)
		return
	}
	sess.MessageID = msg.ID
}

func (b *botState) handleTeamsPairComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	sid, action, ok := parseTpCustomID(data.CustomID)
	if !ok {
		return
	}
	sess := b.pairings.get(sid)
	if sess == nil {
		respondEphemeral(s, i, "This pairing session expired or was cancelled.")
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	uid := interactionUserID(i)
	switch {
	case action == "cancel":
		if uid != sess.CaptainA && uid != sess.CaptainB {
			respondEphemeral(s, i, "Only the captains can cancel.")
			return
		}
		b.pairings.delete(sid)
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Teams pairing cancelled.",
				Components: []discordgo.MessageComponent{},
			},
		})
		return

	case action == "setupA" || action == "setupB":
		b.tpHandleSetupSelect(s, i, sess, action, data.Values)
	case action == "nom":
		b.tpOpenNominate(s, i, sess)
	case action == "nomPick":
		b.tpHandleNominatePick(s, i, sess, data.Values)
	case action == "atk":
		b.tpOpenPropose(s, i, sess)
	case action == "atkPick":
		b.tpHandleProposePick(s, i, sess, data.Values)
	case action == "acc":
		b.tpOpenAccept(s, i, sess)
	case action == "accPick":
		b.tpHandleAcceptPick(s, i, sess, data.Values)
	case strings.HasPrefix(action, "tblPick:"):
		matchIdx := -1
		fmt.Sscanf(strings.TrimPrefix(action, "tblPick:"), "%d", &matchIdx)
		b.tpHandleTablePick(s, i, sess, matchIdx, data.Values)
	default:
		respondEphemeral(s, i, "Unknown pairing action.")
	}
}

func (b *botState) tpHandleSetupSelect(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession, action string, values []string) {
	if sess.Phase != tpPhaseSetup {
		respondEphemeral(s, i, "Rosters are already locked.")
		return
	}
	uid := interactionUserID(i)
	team := "a"
	needCaptain := sess.CaptainA
	if action == "setupB" {
		team = "b"
		needCaptain = sess.CaptainB
	}
	if uid != needCaptain {
		respondEphemeral(s, i, "Only that team's captain can set this roster.")
		return
	}
	if len(values) != sess.Size {
		respondEphemeral(s, i, fmt.Sprintf("Select exactly %d players.", sess.Size))
		return
	}

	mc := i.MessageComponentData()
	players := make([]teamPairPlayer, 0, len(values))
	seen := map[string]bool{}
	for _, id := range values {
		if seen[id] {
			respondEphemeral(s, i, "Duplicate player in selection.")
			return
		}
		seen[id] = true
		name := id
		if mc.Resolved.Members != nil {
			if m := mc.Resolved.Members[id]; m != nil && m.Nick != "" {
				name = m.Nick
			}
		}
		if name == id && mc.Resolved.Users != nil {
			if u := mc.Resolved.Users[id]; u != nil {
				name = displayName(u)
			}
		}
		players = append(players, teamPairPlayer{ID: id, Name: name})
	}

	captainOnRoster := false
	for _, p := range players {
		if p.ID == needCaptain {
			captainOnRoster = true
			break
		}
	}
	if !captainOnRoster {
		respondEphemeral(s, i, "Captain must be included in their own roster.")
		return
	}

	if team == "a" {
		if overlapIDs(players, sess.TeamB) {
			respondEphemeral(s, i, "A player is already on Team B.")
			return
		}
		sess.TeamA = players
	} else {
		if overlapIDs(players, sess.TeamA) {
			respondEphemeral(s, i, "A player is already on Team A.")
			return
		}
		sess.TeamB = players
	}

	if len(sess.TeamA) == sess.Size && len(sess.TeamB) == sess.Size {
		sess.RemainingA = playersToMap(sess.TeamA)
		sess.RemainingB = playersToMap(sess.TeamB)
		sess.Phase = tpPhaseNominate
		sess.resetRound()
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    sess.renderBoard(),
			Components: sess.components(),
		},
	})
}

func overlapIDs(a []teamPairPlayer, b []teamPairPlayer) bool {
	if len(b) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, p := range b {
		set[p.ID] = true
	}
	for _, p := range a {
		if set[p.ID] {
			return true
		}
	}
	return false
}

func playersToMap(ps []teamPairPlayer) map[string]teamPairPlayer {
	m := make(map[string]teamPairPlayer, len(ps))
	for _, p := range ps {
		m[p.ID] = p
	}
	return m
}

func (sess *teamPairSession) resetRound() {
	sess.DefenderA = ""
	sess.DefenderB = ""
	sess.ProposedByA = nil
	sess.ProposedByB = nil
	sess.AcceptedForA = ""
	sess.AcceptedForB = ""
}

func (sess *teamPairSession) remainingList(team string) []teamPairPlayer {
	src := sess.RemainingA
	if team == "b" {
		src = sess.RemainingB
	}
	out := make([]teamPairPlayer, 0, len(src))
	for _, p := range src {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (b *botState) tpOpenNominate(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession) {
	if sess.Phase != tpPhaseNominate {
		respondEphemeral(s, i, "Not in defender nomination phase.")
		return
	}
	uid := interactionUserID(i)
	team, ok := sess.captainTeam(uid)
	if !ok {
		respondEphemeral(s, i, "Only captains nominate defenders.")
		return
	}
	if (team == "a" && sess.DefenderA != "") || (team == "b" && sess.DefenderB != "") {
		respondEphemeral(s, i, "You already nominated a defender this round.")
		return
	}

	// Endgame: 1 left each → auto pair
	if len(sess.RemainingA) == 1 && len(sess.RemainingB) == 1 {
		b.tpAutoPairLast(s, i, sess)
		return
	}

	opts := playerSelectOptions(sess.remainingList(team))
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: "Secretly choose your **Defender** (only you see this):",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:    tpCustomID(sess.ID, "nomPick"),
							Placeholder: "Select defender",
							MinValues:   intPtr(1),
							MaxValues:   1,
							Options:     opts,
						},
					},
				},
			},
		},
	})
}

func (b *botState) tpHandleNominatePick(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession, values []string) {
	if sess.Phase != tpPhaseNominate {
		respondEphemeral(s, i, "Not in defender nomination phase.")
		return
	}
	uid := interactionUserID(i)
	team, ok := sess.captainTeam(uid)
	if !ok || len(values) != 1 {
		respondEphemeral(s, i, "Invalid nomination.")
		return
	}
	defID := values[0]
	pool := sess.RemainingA
	if team == "b" {
		pool = sess.RemainingB
	}
	if _, exists := pool[defID]; !exists {
		respondEphemeral(s, i, "That player is not in your remaining pool.")
		return
	}
	if team == "a" {
		if sess.DefenderA != "" {
			respondEphemeral(s, i, "Already nominated.")
			return
		}
		sess.DefenderA = defID
	} else {
		if sess.DefenderB != "" {
			respondEphemeral(s, i, "Already nominated.")
			return
		}
		sess.DefenderB = defID
	}

	if sess.DefenderA != "" && sess.DefenderB != "" {
		sess.Phase = tpPhasePropose
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Defender locked in. Both captains nominated — revealing on the board.",
				Components: []discordgo.MessageComponent{},
			},
		})
		b.tpRefreshBoard(s, sess)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "Defender locked in secretly. Waiting for the other captain…",
			Components: []discordgo.MessageComponent{},
		},
	})
	b.tpRefreshBoard(s, sess)
}

func (b *botState) tpOpenPropose(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession) {
	if sess.Phase != tpPhasePropose {
		respondEphemeral(s, i, "Not in attacker proposal phase.")
		return
	}
	uid := interactionUserID(i)
	team, ok := sess.captainTeam(uid)
	if !ok {
		respondEphemeral(s, i, "Only captains propose attackers.")
		return
	}
	if (team == "a" && len(sess.ProposedByA) > 0) || (team == "b" && len(sess.ProposedByB) > 0) {
		respondEphemeral(s, i, "You already proposed attackers this round.")
		return
	}

	ownDef := sess.DefenderA
	oppDefName := sess.RemainingB[sess.DefenderB].Name
	if team == "b" {
		ownDef = sess.DefenderB
		oppDefName = sess.RemainingA[sess.DefenderA].Name
	}

	candidates := make([]teamPairPlayer, 0)
	for _, p := range sess.remainingList(team) {
		if p.ID == ownDef {
			continue
		}
		candidates = append(candidates, p)
	}
	need := 2
	if len(candidates) < 2 {
		need = len(candidates)
	}
	if need < 1 {
		respondEphemeral(s, i, "No attackers left to propose.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:   discordgo.MessageFlagsEphemeral,
			Content: fmt.Sprintf("Propose **%d** attacker(s) to face opposing defender **%s**:", need, oppDefName),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:    tpCustomID(sess.ID, "atkPick"),
							Placeholder: "Select attackers",
							MinValues:   intPtr(need),
							MaxValues:   need,
							Options:     playerSelectOptions(candidates),
						},
					},
				},
			},
		},
	})
}

func (b *botState) tpHandleProposePick(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession, values []string) {
	if sess.Phase != tpPhasePropose {
		respondEphemeral(s, i, "Not in attacker proposal phase.")
		return
	}
	uid := interactionUserID(i)
	team, ok := sess.captainTeam(uid)
	if !ok {
		respondEphemeral(s, i, "Only captains propose attackers.")
		return
	}
	ownDef := sess.DefenderA
	pool := sess.RemainingA
	if team == "b" {
		ownDef = sess.DefenderB
		pool = sess.RemainingB
	}
	for _, id := range values {
		if id == ownDef {
			respondEphemeral(s, i, "Cannot propose your defender as an attacker.")
			return
		}
		if _, ok := pool[id]; !ok {
			respondEphemeral(s, i, "Invalid attacker selection.")
			return
		}
	}
	if team == "a" {
		sess.ProposedByA = append([]string{}, values...)
	} else {
		sess.ProposedByB = append([]string{}, values...)
	}

	if len(sess.ProposedByA) > 0 && len(sess.ProposedByB) > 0 {
		sess.Phase = tpPhaseAccept
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Attackers proposed. Moving to accept/refuse.",
				Components: []discordgo.MessageComponent{},
			},
		})
		b.tpRefreshBoard(s, sess)
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "Attackers locked. Waiting for the other captain…",
			Components: []discordgo.MessageComponent{},
		},
	})
	b.tpRefreshBoard(s, sess)
}

func (b *botState) tpOpenAccept(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession) {
	if sess.Phase != tpPhaseAccept {
		respondEphemeral(s, i, "Not in accept/refuse phase.")
		return
	}
	uid := interactionUserID(i)
	team, ok := sess.captainTeam(uid)
	if !ok {
		respondEphemeral(s, i, "Only captains accept attackers.")
		return
	}
	if (team == "a" && sess.AcceptedForA != "") || (team == "b" && sess.AcceptedForB != "") {
		respondEphemeral(s, i, "You already accepted an attacker this round.")
		return
	}

	// Offered against MY defender come from the opposing team.
	offered := sess.ProposedByB
	defName := sess.RemainingA[sess.DefenderA].Name
	if team == "b" {
		offered = sess.ProposedByA
		defName = sess.RemainingB[sess.DefenderB].Name
	}
	opts := make([]discordgo.SelectMenuOption, 0, len(offered))
	oppPool := sess.RemainingB
	if team == "b" {
		oppPool = sess.RemainingA
	}
	for _, id := range offered {
		p := oppPool[id]
		opts = append(opts, discordgo.SelectMenuOption{
			Label: trunc(p.Name, 100),
			Value: id,
		})
	}
	if len(opts) == 0 {
		respondEphemeral(s, i, "No attackers were proposed.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
			Content: fmt.Sprintf(
				"Your defender is **%s**. Accept one attacker (the other is refused back to the pool):",
				defName,
			),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.SelectMenu{
							CustomID:    tpCustomID(sess.ID, "accPick"),
							Placeholder: "Accept one attacker",
							MinValues:   intPtr(1),
							MaxValues:   1,
							Options:     opts,
						},
					},
				},
			},
		},
	})
}

func (b *botState) tpHandleAcceptPick(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession, values []string) {
	if sess.Phase != tpPhaseAccept || len(values) != 1 {
		respondEphemeral(s, i, "Invalid accept.")
		return
	}
	uid := interactionUserID(i)
	team, ok := sess.captainTeam(uid)
	if !ok {
		respondEphemeral(s, i, "Only captains accept attackers.")
		return
	}
	pick := values[0]
	offered := sess.ProposedByB
	if team == "b" {
		offered = sess.ProposedByA
	}
	found := false
	for _, id := range offered {
		if id == pick {
			found = true
			break
		}
	}
	if !found {
		respondEphemeral(s, i, "That attacker was not offered.")
		return
	}
	if team == "a" {
		if sess.AcceptedForA != "" {
			respondEphemeral(s, i, "Already accepted.")
			return
		}
		sess.AcceptedForA = pick
	} else {
		if sess.AcceptedForB != "" {
			respondEphemeral(s, i, "Already accepted.")
			return
		}
		sess.AcceptedForB = pick
	}

	if sess.AcceptedForA == "" || sess.AcceptedForB == "" {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "Attacker accepted. Waiting for the other captain…",
				Components: []discordgo.MessageComponent{},
			},
		})
		b.tpRefreshBoard(s, sess)
		return
	}

	// Lock two matchups; refused attackers stay in remaining pools.
	matchA := teamPairMatch{
		Defender:     sess.RemainingA[sess.DefenderA],
		Attacker:     sess.RemainingB[sess.AcceptedForA],
		DefenderTeam: "a",
	}
	matchB := teamPairMatch{
		Defender:     sess.RemainingB[sess.DefenderB],
		Attacker:     sess.RemainingA[sess.AcceptedForB],
		DefenderTeam: "b",
	}
	delete(sess.RemainingA, sess.DefenderA)
	delete(sess.RemainingA, sess.AcceptedForB)
	delete(sess.RemainingB, sess.DefenderB)
	delete(sess.RemainingB, sess.AcceptedForA)

	sess.Matches = append(sess.Matches, matchA, matchB)
	startIdx := len(sess.Matches) - 2
	sess.PendingTableMatch = startIdx
	sess.Phase = tpPhaseTables

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "Both attackers accepted. Defenders pick tables next.",
			Components: []discordgo.MessageComponent{},
		},
	})
	b.tpRefreshBoard(s, sess)
}

func (b *botState) tpHandleTablePick(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession, matchIdx int, values []string) {
	if sess.Phase != tpPhaseTables {
		respondEphemeral(s, i, "Not in table selection phase.")
		return
	}
	if matchIdx < 0 || matchIdx >= len(sess.Matches) {
		respondEphemeral(s, i, "Invalid match.")
		return
	}
	if matchIdx != sess.PendingTableMatch {
		respondEphemeral(s, i, "Not this defender's turn to pick a table.")
		return
	}
	m := sess.Matches[matchIdx]
	uid := interactionUserID(i)
	if uid != m.Defender.ID && uid != sess.captainForTeam(m.DefenderTeam) {
		respondEphemeral(s, i, "Only the defender (or their captain) can pick this table.")
		return
	}
	if len(values) != 1 {
		respondEphemeral(s, i, "Pick one table.")
		return
	}
	table := values[0]
	found := false
	left := make([]string, 0, len(sess.TablesLeft))
	for _, t := range sess.TablesLeft {
		if t == table && !found {
			found = true
			continue
		}
		left = append(left, t)
	}
	if !found {
		respondEphemeral(s, i, "That table is not available.")
		return
	}
	sess.Matches[matchIdx].Table = table
	sess.TablesLeft = left

	// Next pending match in this round without a table, else advance.
	next := -1
	for idx := matchIdx + 1; idx < len(sess.Matches); idx++ {
		if sess.Matches[idx].Table == "" {
			next = idx
			break
		}
	}
	if next >= 0 {
		sess.PendingTableMatch = next
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    sess.renderBoard(),
				Components: sess.components(),
			},
		})
		return
	}

	sess.PendingTableMatch = -1
	if len(sess.RemainingA) == 0 && len(sess.RemainingB) == 0 {
		sess.Phase = tpPhaseDone
	} else if len(sess.RemainingA) == 1 && len(sess.RemainingB) == 1 {
		// Auto-pair last with last table.
		var a, bPlayer teamPairPlayer
		for _, p := range sess.RemainingA {
			a = p
		}
		for _, p := range sess.RemainingB {
			bPlayer = p
		}
		table := ""
		if len(sess.TablesLeft) > 0 {
			table = sess.TablesLeft[0]
			sess.TablesLeft = sess.TablesLeft[1:]
		}
		sess.Matches = append(sess.Matches, teamPairMatch{
			Defender:     a,
			Attacker:     bPlayer,
			DefenderTeam: "a",
			Table:        table,
		})
		sess.RemainingA = map[string]teamPairPlayer{}
		sess.RemainingB = map[string]teamPairPlayer{}
		sess.Phase = tpPhaseDone
	} else {
		sess.resetRound()
		sess.Phase = tpPhaseNominate
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    sess.renderBoard(),
			Components: sess.components(),
		},
	})
}

func (b *botState) tpAutoPairLast(s *discordgo.Session, i *discordgo.InteractionCreate, sess *teamPairSession) {
	var a, bp teamPairPlayer
	for _, p := range sess.RemainingA {
		a = p
	}
	for _, p := range sess.RemainingB {
		bp = p
	}
	table := ""
	if len(sess.TablesLeft) > 0 {
		table = sess.TablesLeft[0]
		sess.TablesLeft = sess.TablesLeft[1:]
	}
	sess.Matches = append(sess.Matches, teamPairMatch{
		Defender: a, Attacker: bp, DefenderTeam: "a", Table: table,
	})
	sess.RemainingA = map[string]teamPairPlayer{}
	sess.RemainingB = map[string]teamPairPlayer{}
	sess.Phase = tpPhaseDone
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    sess.renderBoard(),
			Components: sess.components(),
		},
	})
}

func (sess *teamPairSession) captainTeam(uid string) (string, bool) {
	if uid == sess.CaptainA {
		return "a", true
	}
	if uid == sess.CaptainB {
		return "b", true
	}
	return "", false
}

func (sess *teamPairSession) captainForTeam(team string) string {
	if team == "b" {
		return sess.CaptainB
	}
	return sess.CaptainA
}

func playerSelectOptions(players []teamPairPlayer) []discordgo.SelectMenuOption {
	opts := make([]discordgo.SelectMenuOption, 0, len(players))
	for _, p := range players {
		opts = append(opts, discordgo.SelectMenuOption{
			Label: trunc(p.Name, 100),
			Value: p.ID,
		})
	}
	return opts
}

func (sess *teamPairSession) renderBoard() string {
	var b strings.Builder
	b.WriteString("**40k Teams Pairing**\n")
	b.WriteString(fmt.Sprintf("Session `%s` · %d per team\n", sess.ID, sess.Size))
	b.WriteString(fmt.Sprintf("Captain A: <@%s> · Captain B: <@%s>\n\n", sess.CaptainA, sess.CaptainB))

	b.WriteString("**Team A**")
	if len(sess.TeamA) == 0 {
		b.WriteString(" — *roster not set*\n")
	} else {
		b.WriteString("\n")
		for _, p := range sess.TeamA {
			mark := ""
			if _, ok := sess.RemainingA[p.ID]; !ok && sess.Phase != tpPhaseSetup {
				mark = " *(paired)*"
			}
			b.WriteString(fmt.Sprintf("• %s%s\n", p.Name, mark))
		}
	}
	b.WriteString("\n**Team B**")
	if len(sess.TeamB) == 0 {
		b.WriteString(" — *roster not set*\n")
	} else {
		b.WriteString("\n")
		for _, p := range sess.TeamB {
			mark := ""
			if _, ok := sess.RemainingB[p.ID]; !ok && sess.Phase != tpPhaseSetup {
				mark = " *(paired)*"
			}
			b.WriteString(fmt.Sprintf("• %s%s\n", p.Name, mark))
		}
	}

	if len(sess.Matches) > 0 {
		b.WriteString("\n**Locked matchups**\n")
		for i, m := range sess.Matches {
			table := m.Table
			if table == "" {
				table = "_table TBD_"
			}
			b.WriteString(fmt.Sprintf("%d. **%s** (def) vs **%s** @ %s\n", i+1, m.Defender.Name, m.Attacker.Name, table))
		}
	}

	b.WriteString("\n**Phase:** ")
	switch sess.Phase {
	case tpPhaseSetup:
		b.WriteString("Captains set rosters (user select below).\n")
	case tpPhaseNominate:
		aReady := "waiting"
		bReady := "waiting"
		if sess.DefenderA != "" {
			aReady = "nominated ✓"
		}
		if sess.DefenderB != "" {
			bReady = "nominated ✓"
		}
		b.WriteString(fmt.Sprintf("Secret defender nomination — A: %s · B: %s\n", aReady, bReady))
		b.WriteString("Captains: press **Nominate defender** (choice is ephemeral/secret).\n")
	case tpPhasePropose:
		b.WriteString(fmt.Sprintf(
			"Defenders revealed — **%s** vs pool / **%s** vs pool.\n",
			sess.RemainingA[sess.DefenderA].Name,
			sess.RemainingB[sess.DefenderB].Name,
		))
		aReady, bReady := "waiting", "waiting"
		if len(sess.ProposedByA) > 0 {
			aReady = "proposed ✓"
		}
		if len(sess.ProposedByB) > 0 {
			bReady = "proposed ✓"
		}
		b.WriteString(fmt.Sprintf("Attacker proposals — A: %s · B: %s\n", aReady, bReady))
	case tpPhaseAccept:
		b.WriteString("Accept/refuse attackers offered against your defender.\n")
		b.WriteString(fmt.Sprintf("Offered vs A defender: %s\n", namesFromIDs(sess.ProposedByB, sess.RemainingB)))
		b.WriteString(fmt.Sprintf("Offered vs B defender: %s\n", namesFromIDs(sess.ProposedByA, sess.RemainingA)))
	case tpPhaseTables:
		if sess.PendingTableMatch >= 0 && sess.PendingTableMatch < len(sess.Matches) {
			m := sess.Matches[sess.PendingTableMatch]
			b.WriteString(fmt.Sprintf(
				"Table pick — <@%s> (or captain) chooses a table for **%s** vs **%s**.\n",
				m.Defender.ID, m.Defender.Name, m.Attacker.Name,
			))
		} else {
			b.WriteString("Table selection.\n")
		}
		if len(sess.TablesLeft) > 0 {
			b.WriteString("Tables left: " + strings.Join(sess.TablesLeft, ", ") + "\n")
		}
	case tpPhaseDone:
		b.WriteString("Complete — all players paired.\n")
	}
	return b.String()
}

func namesFromIDs(ids []string, pool map[string]teamPairPlayer) string {
	if len(ids) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if p, ok := pool[id]; ok {
			parts = append(parts, p.Name)
		} else {
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, ", ")
}

func (sess *teamPairSession) components() []discordgo.MessageComponent {
	rows := []discordgo.MessageComponent{}
	cancelRow := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Style:    discordgo.DangerButton,
				Label:    "Cancel pairing",
				CustomID: tpCustomID(sess.ID, "cancel"),
			},
		},
	}

	switch sess.Phase {
	case tpPhaseSetup:
		min := sess.Size
		if len(sess.TeamA) == 0 {
			rows = append(rows, discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.SelectMenu{
						MenuType:    discordgo.UserSelectMenu,
						CustomID:    tpCustomID(sess.ID, "setupA"),
						Placeholder: fmt.Sprintf("Captain A: select %d Team A players", sess.Size),
						MinValues:   intPtr(min),
						MaxValues:   sess.Size,
					},
				},
			})
		}
		if len(sess.TeamB) == 0 {
			rows = append(rows, discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.SelectMenu{
						MenuType:    discordgo.UserSelectMenu,
						CustomID:    tpCustomID(sess.ID, "setupB"),
						Placeholder: fmt.Sprintf("Captain B: select %d Team B players", sess.Size),
						MinValues:   intPtr(min),
						MaxValues:   sess.Size,
					},
				},
			})
		}
		rows = append(rows, cancelRow)

	case tpPhaseNominate:
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Style:    discordgo.PrimaryButton,
					Label:    "Nominate defender",
					CustomID: tpCustomID(sess.ID, "nom"),
				},
			},
		}, cancelRow)

	case tpPhasePropose:
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Style:    discordgo.PrimaryButton,
					Label:    "Propose attackers",
					CustomID: tpCustomID(sess.ID, "atk"),
				},
			},
		}, cancelRow)

	case tpPhaseAccept:
		rows = append(rows, discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Style:    discordgo.PrimaryButton,
					Label:    "Accept an attacker",
					CustomID: tpCustomID(sess.ID, "acc"),
				},
			},
		}, cancelRow)

	case tpPhaseTables:
		if sess.PendingTableMatch >= 0 && sess.PendingTableMatch < len(sess.Matches) && len(sess.TablesLeft) > 0 {
			opts := make([]discordgo.SelectMenuOption, 0, len(sess.TablesLeft))
			for _, t := range sess.TablesLeft {
				opts = append(opts, discordgo.SelectMenuOption{Label: trunc(t, 100), Value: t})
			}
			rows = append(rows, discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.SelectMenu{
						CustomID:    tpCustomID(sess.ID, fmt.Sprintf("tblPick:%d", sess.PendingTableMatch)),
						Placeholder: "Defender: pick a table",
						MinValues:   intPtr(1),
						MaxValues:   1,
						Options:     opts,
					},
				},
			})
		}
		rows = append(rows, cancelRow)

	case tpPhaseDone:
		// no controls
	}
	return rows
}

func (b *botState) tpRefreshBoard(s *discordgo.Session, sess *teamPairSession) {
	if sess.ChannelID == "" || sess.MessageID == "" {
		return
	}
	content := sess.renderBoard()
	components := sess.components()
	_, err := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    sess.ChannelID,
		ID:         sess.MessageID,
		Content:    &content,
		Components: &components,
	})
	if err != nil {
		log.Printf("teams-pair refresh: %v", err)
	}
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func optUserResolved(i *discordgo.InteractionCreate, name string) (*discordgo.User, bool) {
	data := i.ApplicationCommandData()
	for _, o := range data.Options {
		if o.Name != name {
			continue
		}
		id, _ := o.Value.(string)
		if data.Resolved != nil && data.Resolved.Users != nil {
			if u := data.Resolved.Users[id]; u != nil {
				return u, true
			}
		}
		return &discordgo.User{ID: id}, id != ""
	}
	return nil, false
}
