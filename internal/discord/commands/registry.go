package commands

import (
	"context"
	"fmt"
	"strings"

	"characterllm/internal/audit"
	"characterllm/internal/config"
	"characterllm/internal/images"
	"characterllm/internal/llm"
	"characterllm/internal/research"
	"characterllm/internal/session"

	"github.com/bwmarrin/discordgo"
)

// Deps holds the dependencies of the command registry.
type Deps struct {
	Session     *session.Manager
	LLM         llm.LLMClient
	Model       string
	Audit       *audit.AuditLogger
	ImageClient images.ImageClient
	Synthesizer research.Synthesizer
	Config      *config.Config
	Lock        func(guildID, threadID string) func()
}

// ErrUnknownCommand is returned by Registry.Execute for unregistered command names.
var ErrUnknownCommand = fmt.Errorf("unknown command")

type slashCommand interface {
	Definition() *discordgo.ApplicationCommand
	Execute(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) error
}

// componentFunc handles one message component interaction.
type componentFunc func(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate)

// prefixRoute routes component custom IDs sharing a prefix to one handler.
type prefixRoute struct {
	prefix string
	handle componentFunc
}

// Registry is the constructed registry of the bot's commands: named slash
// commands plus the single dispatch point for message component interactions.
type Registry struct {
	session           *session.Manager
	byName            map[string]slashCommand
	componentRoutes   map[string]componentFunc
	componentPrefixes []prefixRoute
	defs              []*discordgo.ApplicationCommand
}

// New builds the command registry; it is the single place listing the bot's commands.
func New(d Deps) *Registry {
	setCharacter := &setCharacterCmd{session: d.Session, imageClient: d.ImageClient}
	createCharacter := &createCharacterCmd{session: d.Session, imageClient: d.ImageClient, synthesizer: d.Synthesizer, audit: d.Audit, config: d.Config}
	deleteCharacter := &deleteCharacterCmd{session: d.Session, imageClient: d.ImageClient}
	editCharacter := &editCharacterCmd{session: d.Session, imageClient: d.ImageClient, synthesizer: d.Synthesizer, audit: d.Audit}
	viewCharacter := &viewCharacterCmd{session: d.Session, imageClient: d.ImageClient}

	registry := &Registry{
		session: d.Session,
		byName:  make(map[string]slashCommand),
		componentRoutes: map[string]componentFunc{
			setCharacterImageID: createCharacter.handleImageSelection,
		},
		componentPrefixes: []prefixRoute{
			{prefix: deleteConfirmPrefix, handle: deleteCharacter.handleDeleteConfirm},
			{prefix: deleteCancelPrefix, handle: deleteCharacter.handleDeleteCancel},
			{prefix: editAcceptPrefix, handle: editCharacter.handleEditAccept},
			{prefix: editRejectPrefix, handle: editCharacter.handleEditReject},
		},
	}
	for _, cmd := range []slashCommand{
		&resetChatCmd{session: d.Session, lock: d.Lock},
		&statusCmd{llm: d.LLM},
		setCharacter,
		viewCharacter,
		createCharacter,
		deleteCharacter,
		editCharacter,
		&setAvatarCmd{session: d.Session, imageClient: d.ImageClient},
	} {
		def := cmd.Definition()
		registry.byName[def.Name] = cmd
		registry.defs = append(registry.defs, def)
	}
	return registry
}

// Definitions returns all slash command definitions for Discord registration.
func (r *Registry) Definitions() []*discordgo.ApplicationCommand { return r.defs }

// Execute dispatches a slash command interaction by name.
func (r *Registry) Execute(ctx context.Context, name string, s DiscordSession, i *discordgo.InteractionCreate) error {
	cmd, ok := r.byName[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownCommand, name)
	}
	return cmd.Execute(ctx, s, i)
}

// HandleAutocomplete responds to autocomplete interactions on the character
// name options by suggesting the guild's saved characters.
func (r *Registry) HandleAutocomplete(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if len(data.Options) == 0 || !data.Options[0].Focused {
		return
	}

	// The "current (active character)" suggestion only makes sense for the
	// commands that accept that key.
	includeCurrent := data.Name == "viewcharacterdetails" || data.Name == "deletecharacter" || data.Name == "editcharacter"
	choices := autocompleteCharacters(ctx, r.session, i.GuildID, data.Options[0].StringValue(), includeCurrent)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

// HandleComponent dispatches message component interactions (buttons, select menus)
// to the command that owns the interaction's custom ID.
func (r *Registry) HandleComponent(ctx context.Context, s DiscordSession, i *discordgo.InteractionCreate) {
	id := i.MessageComponentData().CustomID
	if handle, ok := r.componentRoutes[id]; ok {
		handle(ctx, s, i)
		return
	}
	for _, route := range r.componentPrefixes {
		if strings.HasPrefix(id, route.prefix) {
			route.handle(ctx, s, i)
			return
		}
	}
}
