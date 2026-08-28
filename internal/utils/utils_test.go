package utils

import (
	"encoding/base64"
	"fmt"
	"strings"
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
		name     string
		charName string
		taken    map[string]bool
		check    func(t *testing.T, got string)
	}{
		{
			name:     "Basic slug format",
			charName: "Character Name",
			check: func(t *testing.T, got string) {
				if !strings.HasPrefix(got, "character-name-") || len(got) != len("character-name-")+4 {
					t.Errorf("unexpected slug format: %q", got)
				}
			},
		},
		{
			name:     "Punctuation collapsed to dashes",
			charName: "Spider-Man",
			check: func(t *testing.T, got string) {
				if !strings.HasPrefix(got, "spider-man-") {
					t.Errorf("unexpected prefix: %q", got)
				}
			},
		},
		{
			name:     "Case folded",
			charName: "CHARACTER NAME",
			check: func(t *testing.T, got string) {
				if !strings.HasPrefix(got, "character-name-") {
					t.Errorf("unexpected prefix: %q", got)
				}
			},
		},
		{
			name:     "Empty name yields number only",
			charName: "",
			check: func(t *testing.T, got string) {
				if len(got) != 4 {
					t.Errorf("expected bare 4-digit ID, got %q", got)
				}
			},
		},
		{
			name:     "Avoids taken IDs",
			charName: "Miles Morales",
			taken:    map[string]bool{"miles-morales-0000": true},
			check: func(t *testing.T, got string) {
				if got == "miles-morales-0000" {
					t.Errorf("minted a taken ID: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, CreateCharacterSlug(tt.charName, tt.taken))
		})
	}
}

func TestCreateCharacterSlug_Properties(t *testing.T) {
	// The suffix is a 4-digit decimal, unique against the taken set.
	for i := 0; i < 50; i++ {
		got := CreateCharacterSlug("Miles Morales", nil)
		if !strings.HasPrefix(got, "miles-morales-") {
			t.Fatalf("unexpected slug %q", got)
		}
		suffix := got[len("miles-morales-"):]
		if len(suffix) != 4 {
			t.Fatalf("suffix must be 4 digits: %q", got)
		}
		for _, r := range suffix {
			if r < '0' || r > '9' {
				t.Fatalf("suffix contains non-digit: %q", got)
			}
		}
	}

	// A fully taken suffix space with a short prefix would loop; with 10000
	// slots and a realistic card count, exhaustion is not a concern. Verify
	// the retry works when most of a small prefix's space is taken.
	taken := make(map[string]bool)
	for n := 0; n < 9999; n++ {
		taken[fmt.Sprintf("m-%04d", n)] = true
	}
	got := CreateCharacterSlug("M", taken)
	if got != "m-9999" {
		t.Errorf("expected the single free ID m-9999, got %q", got)
	}

	// Bounded length, filesystem/Discord-safe.
	long := CreateCharacterSlug(strings.Repeat("a", 200), nil)
	if len(long) > 37 {
		t.Errorf("slug exceeds bound: %d chars (%q)", len(long), long)
	}
	for _, r := range long {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			t.Fatalf("slug contains unsafe rune %q: %q", r, long)
		}
	}
}

func TestPtrString(t *testing.T) {
	s := "test"
	ptr := PtrString(s)
	if ptr == nil || *ptr != s {
		t.Errorf("PtrString(%q) = %v; want %q", s, ptr, s)
	}
}

func TestPNGDataURI(t *testing.T) {
	in := []byte{0x89, 'P', 'N', 'G', '\r', '\n'}
	got := PNGDataURI(in)
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(in)
	if got != want {
		t.Errorf("PNGDataURI = %q; want %q", got, want)
	}
}
