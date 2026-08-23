package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	avatarUpdateTimeout           = 10 * time.Second
	MaxSelectMenuDescriptionLength = 100
)

// updateGuildAvatar updates the bot's avatar for a specific guild using a base64 data URI.
func updateGuildAvatar(s *discordgo.Session, guildID, avatarDataURI string) error {
	url := fmt.Sprintf("https://discord.com/api/v10/guilds/%s/members/@me", guildID)
	payload := map[string]string{"avatar": avatarDataURI}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	// Ensure the token is present
	if s.Token == "" {
		return fmt.Errorf("bot token is empty; cannot authenticate request")
	}

	token := s.Token
	if strings.HasPrefix(token, "Bot ") {
		token = strings.TrimPrefix(token, "Bot ")
	}

	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: avatarUpdateTimeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			bodyBytes, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("discord API returned status %d: %s", resp.StatusCode, string(bodyBytes))
		}
		return fmt.Errorf("discord API returned status %d", resp.StatusCode)
	}
	return nil
}
