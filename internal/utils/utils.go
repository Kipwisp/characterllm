package utils

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"strings"
)

// PNGDataURI encodes PNG bytes as a data URI for a vision model.
func PNGDataURI(b []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}

// slugPrefixLimit bounds the readable portion of a character slug.
const slugPrefixLimit = 32

// TruncateString shortens a string to a maximum length and appends an ellipsis if truncated.
func TruncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// CreateCharacterSlug mints a character ID: a readable slugified prefix of
// the name plus a 4-digit number drawn until it is unique among the given
// taken IDs.
func CreateCharacterSlug(name string, taken map[string]bool) string {
	prefix := slugify(name)
	for {
		id := fmt.Sprintf("%04d", rand.Intn(10000))
		if prefix != "" {
			id = prefix + "-" + id
		}
		if !taken[id] {
			return id
		}
	}
}

// slugify reduces s to a lowercase ASCII [a-z0-9-] string with at most
// slugPrefixLimit characters.
func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	p := b.String()
	if len(p) > slugPrefixLimit {
		p = strings.TrimRight(p[:slugPrefixLimit], "-")
	}
	return p
}

// PtrString takes a string and returns a pointer to it.
func PtrString(s string) *string {
	return &s
}
