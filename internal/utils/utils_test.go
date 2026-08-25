package utils

import (
	"testing"
)

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{"No truncation", "Hello", 10, "Hello"},
		{"Truncation", "Hello World", 8, "Hello..."},
		{"Exact length", "Hello World", 11, "Hello World"},
		{"Very short max", "Hello World", 3, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateString(tt.input, tt.max)
			if got != tt.expected {
				t.Errorf("TruncateString(%q, %d) = %q; want %q", tt.input, tt.max, got, tt.expected)
			}
		})
	}
}

func TestCreateCharacterSlug(t *testing.T) {
	tests := []struct {
		name       string
		charName   string
		modifiers  []string
		scenarioID string
		expected   string
	}{
		{
			"Basic slug",
			"Character Name",
			[]string{"Modifier 1", "Modifier 2"},
			"scenario-123",
			"modifier_1_modifier_2##character_name##scenario-123",
		},
		{
			"Sorted modifiers",
			"Character Name",
			[]string{"Z Modifier", "A Modifier"},
			"scenario-123",
			"a_modifier_z_modifier##character_name##scenario-123",
		},
		{
			"No modifiers",
			"Character Name",
			[]string{},
			"scenario-123",
			"##character_name##scenario-123",
		},
		{
			"Case insensitivity",
			"CHARACTER NAME",
			[]string{"MODIFIER"},
			"SCENARIO",
			"modifier##character_name##scenario",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CreateCharacterSlug(tt.charName, tt.modifiers, tt.scenarioID)
			if got != tt.expected {
				t.Errorf("CreateCharacterSlug(%q, %v, %q) = %q; want %q", tt.charName, tt.modifiers, tt.scenarioID, got, tt.expected)
			}
		})
	}
}

func TestPtrString(t *testing.T) {
	s := "test"
	ptr := PtrString(s)
	if ptr == nil || *ptr != s {
		t.Errorf("PtrString(%q) = %v; want %q", s, ptr, s)
	}
}
