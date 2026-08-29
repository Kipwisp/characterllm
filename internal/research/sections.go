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
	SectionRole       = "Role & Relationships"
	SectionVoice      = "Voice & Habits"
	SectionDialogue   = "Example Dialogue"
	SectionScenario   = "Scenario"
	SectionGreeting   = "Greeting"
)

// PersonaSectionOrder lists the always-present persona specification sections
// in output order.
var PersonaSectionOrder = []string{SectionIdentity, SectionAppearance, SectionRole, SectionVoice, SectionDialogue, SectionGreeting}

// cannedSectionDefinitions holds the format definition for sections that are
// emitted only conditionally at synthesis time and so are not part of the
// synthesis prompt's static Output Structure.
var cannedSectionDefinitions = map[string]string{
	SectionScenario: "A short, concrete description of the character's current, temporary circumstances \u2014 the specific situation, place, and moment the conversation is taking place in right now. This is temporary context, not a permanent trait.",
}

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

// sectionDefinitionsFrom resolves the named sections' definitions — from the
// synthesis prompt when present there, otherwise from cannedSectionDefinitions
// for conditionally-emitted sections — and joins them with a blank line,
// reattaching each "### " header and dropping any lone {{PLACEHOLDER}} lines
// the prompt's conditional blocks leave inside a section's span.
func sectionDefinitionsFrom(prompt string, sections []string) string {
	var b strings.Builder
	for _, s := range sections {
		body, ok := ExtractSection(prompt, s)
		if !ok {
			body = cannedSectionDefinitions[s]
		}
		body = strings.TrimSpace(stripPlaceholderLines(body))
		if body == "" {
			continue
		}
		b.WriteString(SectionHeaderPrefix + s + "\n")
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// stripPlaceholderLines removes lines that consist solely of a
// {{PLACEHOLDER}} token.
func stripPlaceholderLines(s string) string {
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "{{") && strings.HasSuffix(t, "}}") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
