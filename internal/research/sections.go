package research

import (
	"fmt"
	"strings"
)

// Persona specification section names, matching the synthesis prompt's
// Output Structure. The prompt file is the source of truth for section
// order; these constants define the grammar (header matching and body
// boundaries) shared by synthesis parsing and section-level edits.

// SectionHeaderPrefix is the markdown prefix that begins every persona
// specification section header line.
const SectionHeaderPrefix = "### "

const (
	SectionIdentity   = "Identity & Temperament"
	SectionAppearance = "Appearance"
	SectionVoice      = "Voice & Habits"
	SectionDialogue   = "Example Dialogue"
	SectionScenario   = "Scenario"
	SectionGreeting   = "Greeting"
)

// sectionBounds returns the start and end offsets of the body of the named
// section: after the "### <section>" header line and before the next
// "### " header (or the end of the spec).
func sectionBounds(spec, section string) (int, int, bool) {
	header := SectionHeaderPrefix + section + "\n"
	var bodyStart int
	if strings.HasPrefix(spec, header) {
		bodyStart = len(header)
	} else if idx := strings.Index(spec, "\n"+header); idx != -1 {
		bodyStart = idx + 1 + len(header)
	} else {
		return 0, 0, false
	}

	bodyEnd := len(spec)
	if idx := strings.Index(spec[bodyStart:], "\n### "); idx != -1 {
		bodyEnd = bodyStart + idx
	}
	return bodyStart, bodyEnd, true
}

// ExtractSection returns the body of the named section without surrounding
// blank lines, and whether the section exists.
func ExtractSection(spec, section string) (string, bool) {
	start, end, ok := sectionBounds(spec, section)
	if !ok {
		return "", false
	}
	return strings.TrimRight(spec[start:end], "\n"), true
}

// ReplaceSection returns the spec with the named section (header and body)
// replaced by a fresh section containing body, normalizing the spacing around
// it to a single blank line. A missing section is appended at the end.
func ReplaceSection(spec, section, body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("empty body for section %s", section)
	}

	newSection := SectionHeaderPrefix + section + "\n" + body + "\n"
	cut, end, ok := sectionCut(spec, section)
	if !ok {
		spec = strings.TrimRight(spec, "\n")
		if spec == "" {
			return newSection, nil
		}
		return spec + "\n\n" + newSection, nil
	}

	head := strings.TrimRight(spec[:cut], "\n")
	tail := strings.TrimLeft(spec[end:], "\n")
	switch {
	case head == "" && tail == "":
		return newSection, nil
	case head == "":
		return newSection + "\n" + tail, nil
	case tail == "":
		return head + "\n\n" + newSection, nil
	default:
		return head + "\n\n" + newSection + "\n" + tail, nil
	}
}

// RemoveSection returns the spec with the named section (header and body)
// deleted, joining the surrounding content with a single blank line.
func RemoveSection(spec, section string) string {
	cut, end, ok := sectionCut(spec, section)
	if !ok {
		return spec
	}
	head := strings.TrimRight(spec[:cut], "\n")
	tail := strings.TrimLeft(spec[end:], "\n")
	switch {
	case head == "":
		return tail
	case tail == "":
		return head
	default:
		return head + "\n\n" + tail
	}
}

// sectionCut returns the offset of the start of the named section's header
// line and the end of its body, for removal/replacement purposes.
func sectionCut(spec, section string) (int, int, bool) {
	bodyStart, bodyEnd, ok := sectionBounds(spec, section)
	if !ok {
		return 0, 0, false
	}
	// Walk back over the header line and the newline preceding it when
	// the section is not at the start of the spec.
	cut := bodyStart - 1 - len(SectionHeaderPrefix+section+"\n")
	if cut < 0 {
		cut = 0
	}
	return cut, bodyEnd, true
}

// SpecSection is one "### " section of a persona specification, in order.
// The Name is empty for the preamble (text before the first header).
type SpecSection struct {
	Name string
	Body string
}

// SplitSections splits a persona specification into its ordered "### "
// sections, trimming each body of surrounding blank lines.
func SplitSections(spec string) []SpecSection {
	var sections []SpecSection
	var name string
	var body []string
	flush := func() {
		trimmed := strings.TrimSpace(strings.Join(body, "\n"))
		if name != "" || trimmed != "" {
			sections = append(sections, SpecSection{Name: name, Body: trimmed})
		}
		name = ""
		body = nil
	}
	for _, line := range strings.Split(spec, "\n") {
		if strings.HasPrefix(line, SectionHeaderPrefix) {
			flush()
			name = strings.TrimSpace(strings.TrimPrefix(line, SectionHeaderPrefix))
			continue
		}
		body = append(body, line)
	}
	flush()
	return sections
}
