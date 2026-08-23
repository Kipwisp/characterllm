package utils

import (
	"fmt"
	"sort"
	"strings"
)

// TruncateString shortens a string to a maximum length and appends an ellipsis if truncated.
func TruncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// CreateCharacterSlug generates a canonical lowercase slug for a character identity using unique section delimiters to prevent collisions.
func CreateCharacterSlug(name string, modifiers []string, scenarioID string) string {
	sortedMods := make([]string, len(modifiers))
	copy(sortedMods, modifiers)
	sort.Strings(sortedMods)

	modsSlug := strings.ToLower(strings.Join(sortedMods, "_"))
	nameSlug := strings.ToLower(strings.ReplaceAll(name, " ", "_"))
	scenarioSlug := strings.ToLower(scenarioID)

	// Format: [modifiers]##[name]##[scenario]
	return fmt.Sprintf("%s##%s##%s", modsSlug, nameSlug, scenarioSlug)
}

// PtrString takes a string and returns a pointer to it.
func PtrString(s string) *string {
	return &s
}
