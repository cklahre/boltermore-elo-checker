package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func boltermoreWebURL() string {
	if v := strings.TrimSpace(os.Getenv("BOLTERMORE_WEB_URL")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return "http://127.0.0.1:3000"
}

func boltermoreAPISecret() string {
	return strings.TrimSpace(os.Getenv("DISCORD_BOT_API_SECRET"))
}

type challengeAPIResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	Challenge struct {
		Status           string `json:"status"`
		DefenderFaction  string `json:"defenderFaction"`
		ChallengerUserID string `json:"challengerUserId"`
		DefenderUserID   string `json:"defenderUserId"`
	} `json:"challenge"`
}

func callChallengeAPI(action, boardID, challengeID, discordUserID string, extra map[string]any) (challengeAPIResponse, error) {
	secret := boltermoreAPISecret()
	if secret == "" {
		return challengeAPIResponse{}, fmt.Errorf("DISCORD_BOT_API_SECRET is not configured")
	}
	payload := map[string]any{
		"boardId":       boardID,
		"challengeId":   challengeID,
		"action":        action,
		"discordUserId": discordUserID,
	}
	for k, v := range extra {
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return challengeAPIResponse{}, err
	}
	req, err := http.NewRequest(http.MethodPost, boltermoreWebURL()+"/api/internal/discord/challenge", bytes.NewReader(raw))
	if err != nil {
		return challengeAPIResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return challengeAPIResponse{}, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var out challengeAPIResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return challengeAPIResponse{}, fmt.Errorf("decode response: %w (body %s)", err, trunc(string(body), 200))
	}
	if res.StatusCode >= 400 || !out.OK {
		if out.Error != "" {
			return out, fmt.Errorf("%s", out.Error)
		}
		return out, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	return out, nil
}

func discordUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func (b *botState) onChallengeInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		return b.onChallengeButton(s, i)
	case discordgo.InteractionModalSubmit:
		return b.onChallengeModal(s, i)
	default:
		return false
	}
}

func parseChallengeCustomID(customID string) (kind, boardID, challengeID string, ok bool) {
	parts := strings.Split(customID, ":")
	if len(parts) < 4 || parts[0] != "ch" {
		return "", "", "", false
	}
	return parts[1], parts[2], parts[3], true
}

func (b *botState) onChallengeButton(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	data := i.MessageComponentData()
	kind, boardID, challengeID, ok := parseChallengeCustomID(data.CustomID)
	if !ok {
		return false
	}
	userID := discordUserID(i)
	if userID == "" {
		return true
	}

	switch kind {
	case "a":
		// Accept — if defender needs faction, open modal first.
		// We don't know faction state here without an API read; always offer modal when label says pick faction,
		// otherwise try direct accept and fall back to modal on "Faction is required".
		resp, err := callChallengeAPI("accept", boardID, challengeID, userID, nil)
		if err != nil && strings.Contains(strings.ToLower(err.Error()), "faction") {
			return b.respondFactionModal(s, i, boardID, challengeID)
		}
		if err != nil {
			b.respondEphemeral(s, i, "Could not accept: "+err.Error())
			return true
		}
		msg := "Challenge accepted."
		if resp.Challenge.Status == "accepted" {
			msg = "Challenge accepted — play your game, then use **Report result**."
		}
		b.respondEphemeral(s, i, msg)
		return true
	case "d":
		_, err := callChallengeAPI("decline", boardID, challengeID, userID, nil)
		if err != nil {
			b.respondEphemeral(s, i, "Could not decline: "+err.Error())
			return true
		}
		b.respondEphemeral(s, i, "Challenge declined (counts as a loss).")
		return true
	case "r":
		return b.respondResultModal(s, i, boardID, challengeID)
	default:
		return false
	}
}

func (b *botState) respondFactionModal(s *discordgo.Session, i *discordgo.InteractionCreate, boardID, challengeID string) bool {
	customID := fmt.Sprintf("ch:mf:%s:%s", boardID, challengeID)
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: customID,
			Title:    "Accept challenge",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "faction",
							Label:       "Your faction",
							Style:       discordgo.TextInputShort,
							Placeholder: "e.g. Adeptus Custodes",
							Required:    true,
							MaxLength:   80,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("challenge faction modal: %v", err)
	}
	return true
}

func (b *botState) respondResultModal(s *discordgo.Session, i *discordgo.InteractionCreate, boardID, challengeID string) bool {
	customID := fmt.Sprintf("ch:mr:%s:%s", boardID, challengeID)
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: customID,
			Title:    "Report result",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "challenger_vp",
							Label:     "Challenger VP",
							Style:     discordgo.TextInputShort,
							Required:  true,
							MaxLength: 3,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:  "defender_vp",
							Label:     "Defender VP",
							Style:     discordgo.TextInputShort,
							Required:  true,
							MaxLength: 3,
						},
					},
				},
			},
		},
	})
	if err != nil {
		log.Printf("challenge result modal: %v", err)
	}
	return true
}

func (b *botState) onChallengeModal(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	data := i.ModalSubmitData()
	kind, boardID, challengeID, ok := parseChallengeCustomID(data.CustomID)
	if !ok {
		return false
	}
	userID := discordUserID(i)
	if userID == "" {
		return true
	}

	switch kind {
	case "mf":
		faction := modalTextValue(data, 0)
		_, err := callChallengeAPI("accept", boardID, challengeID, userID, map[string]any{"faction": faction})
		if err != nil {
			b.respondEphemeral(s, i, "Could not accept: "+err.Error())
			return true
		}
		b.respondEphemeral(s, i, "Challenge accepted — play your game, then use **Report result**.")
		return true
	case "mr":
		chVP, err1 := strconv.Atoi(strings.TrimSpace(modalTextValue(data, 0)))
		defVP, err2 := strconv.Atoi(strings.TrimSpace(modalTextValue(data, 1)))
		if err1 != nil || err2 != nil {
			b.respondEphemeral(s, i, "Victory points must be numbers.")
			return true
		}
		_, err := callChallengeAPI("result", boardID, challengeID, userID, map[string]any{
			"challengerVp": chVP,
			"defenderVp":   defVP,
		})
		if err != nil {
			b.respondEphemeral(s, i, "Could not post result: "+err.Error())
			return true
		}
		b.respondEphemeral(s, i, "Result posted — the board has been updated.")
		return true
	default:
		return false
	}
}

func (b *botState) respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if len(content) > 1900 {
		content = content[:1900] + "…"
	}
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Printf("challenge ephemeral respond: %v", err)
	}
}

func modalTextValue(data discordgo.ModalSubmitInteractionData, rowIndex int) string {
	if rowIndex >= len(data.Components) {
		return ""
	}
	row, ok := data.Components[rowIndex].(*discordgo.ActionsRow)
	if !ok || len(row.Components) == 0 {
		return ""
	}
	input, ok := row.Components[0].(*discordgo.TextInput)
	if !ok {
		return ""
	}
	return strings.TrimSpace(input.Value)
}
