package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"characterllm/internal/discord/commands"
	"github.com/bwmarrin/discordgo"
)

const avatarUpdateTimeout = 10 * time.Second

type sessionWrapper struct {
	s *discordgo.Session
}

func NewSessionWrapper(s *discordgo.Session) commands.DiscordSession {
	return &sessionWrapper{s: s}
}

func (w *sessionWrapper) ChannelTyping(channelID string) error {
	return w.s.ChannelTyping(channelID)
}

func (w *sessionWrapper) ChannelMessageSend(channelID string, content string) (*discordgo.Message, error) {
	return w.s.ChannelMessageSend(channelID, content)
}

func (w *sessionWrapper) ChannelMessageSendReply(channelID string, content string, response *discordgo.MessageReference) (*discordgo.Message, error) {
	return w.s.ChannelMessageSendReply(channelID, content, response)
}

func (w *sessionWrapper) ChannelMessageSendComplex(channelID string, msg *discordgo.MessageSend) (*discordgo.Message, error) {
	return w.s.ChannelMessageSendComplex(channelID, msg)
}

func (w *sessionWrapper) ChannelMessageEditComplex(channelID, messageID string, edit *discordgo.MessageEdit) (*discordgo.Message, error) {
	edit.Channel = channelID
	edit.ID = messageID
	return w.s.ChannelMessageEditComplex(edit)
}

func (w *sessionWrapper) ChannelMessageDelete(channelID, messageID string) error {
	return w.s.ChannelMessageDelete(channelID, messageID)
}

func (w *sessionWrapper) ChannelMessages(channelID string, limit int, beforeID, afterID, aroundID string) ([]*discordgo.Message, error) {
	return w.s.ChannelMessages(channelID, limit, beforeID, afterID, aroundID)
}

func (w *sessionWrapper) GuildChannels(guildID string) ([]*discordgo.Channel, error) {
	return w.s.GuildChannels(guildID)
}

func (w *sessionWrapper) InteractionRespond(interaction *discordgo.Interaction, response *discordgo.InteractionResponse) error {
	return w.s.InteractionRespond(interaction, response)
}

func (w *sessionWrapper) InteractionResponseEdit(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) (*discordgo.Message, error) {
	return w.s.InteractionResponseEdit(interaction, edit)
}

func (w *sessionWrapper) GuildMemberNickname(guildID string, member string, nickname string) error {
	return w.s.GuildMemberNickname(guildID, member, nickname)
}

func (w *sessionWrapper) UpdateGuildAvatar(guildID, avatarDataURI string) error {
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

	token := w.s.Token
	if token == "" {
		return fmt.Errorf("bot token is empty; cannot authenticate request")
	}
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

func (w *sessionWrapper) GetUserMention() string {
	return w.s.State.User.Mention()
}

func (w *sessionWrapper) GetUserID() string {
	return w.s.State.User.ID
}

func (w *sessionWrapper) GetToken() string {
	return w.s.Token
}
