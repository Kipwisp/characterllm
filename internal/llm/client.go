// Package llm provides a client for interacting with LLM servers (e.g. llama.cpp).
package llm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PartKind selects the kind of content a Part carries.
type PartKind string

const (
	PartText  PartKind = "text"
	PartImage PartKind = "image"
)

// Part is one element of a message's content, in the order it should be
// sent: a stretch of text or a single image.
type Part struct {
	Kind PartKind
	Text string // PartText
	// ImageURL holds a data URI for PartImage.
	ImageURL string
}

// Role identifies who produced a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
	RoleTool      Role = "tool"
)

// String returns the wire/stored representation of the role.
func (r Role) String() string { return string(r) }

// ParseRole validates a role read from storage or the wire.
func ParseRole(s string) (Role, error) {
	switch Role(s) {
	case RoleUser, RoleAssistant, RoleSystem, RoleTool:
		return Role(s), nil
	}
	return "", fmt.Errorf("unknown role %q", s)
}

// Message is one turn in an LLM conversation: a role and an ordered list of
// content parts (text or image).
type Message struct {
	Role      Role
	Parts     []Part
	Reasoning string
}

// TextMessage builds a message with a single text part.
func TextMessage(role Role, text string) Message {
	return Message{Role: role, Parts: []Part{{Kind: PartText, Text: text}}}
}

// TextWithImages builds ordered parts: the text, then one image part per
// data URI.
func TextWithImages(text string, imageURIs []string) []Part {
	parts := make([]Part, 0, len(imageURIs)+1)
	parts = append(parts, Part{Kind: PartText, Text: text})
	for _, u := range imageURIs {
		parts = append(parts, Part{Kind: PartImage, ImageURL: u})
	}
	return parts
}

// Text returns the message's text: its text parts joined (image parts
// contribute nothing).
func (m Message) Text() string {
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Kind == PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// ImageURIs returns the data URIs of the message's image parts, in order.
func (m Message) ImageURIs() []string {
	var uris []string
	for _, p := range m.Parts {
		if p.Kind == PartImage {
			uris = append(uris, p.ImageURL)
		}
	}
	return uris
}

// LLMClient defines the interface for interacting with an LLM server.
type LLMClient interface {
	Ping(ctx context.Context) (time.Duration, error)
	GenerateResponse(ctx context.Context, messages []Message, model string) (string, string, error)
	EstimateTokens(ctx context.Context, messages []Message) int
}
