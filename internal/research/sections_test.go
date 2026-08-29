package research

import (
	"strings"
	"testing"
)

const testSpec = "### Identity & Temperament\nCold and questioning because of a decade in exile.\n\n### Appearance\n- **Species/Origin**: Human\n- **Eyes/Hair**: Grey eyes, white hair\n\n### Role & Relationships\n- **Role/Occupation**: Cartographer\n\n### Voice & Habits\nSlow cadence, dry wit.\n\n### Example Dialogue\n<START>\nUser: Hello\nCharacter: You again.\n"

func TestExtractSection(t *testing.T) {
	body, ok := ExtractSection(testSpec, SectionAppearance)
	if !ok {
		t.Fatal("expected Appearance section to exist")
	}
	want := "- **Species/Origin**: Human\n- **Eyes/Hair**: Grey eyes, white hair"
	if body != want {
		t.Errorf("Appearance body = %q; want %q", body, want)
	}

	body, ok = ExtractSection(testSpec, SectionIdentity)
	if !ok || !strings.HasPrefix(body, "Cold and questioning") {
		t.Errorf("first section (no leading newline) not extracted: ok=%v body=%q", ok, body)
	}

	if _, ok := ExtractSection(testSpec, "Personality"); ok {
		t.Error("expected missing section to report not found")
	}

	// A section name that is a prefix of another must not match.
	spec := "### Voice\nshort body\n"
	if _, ok := ExtractSection(spec, SectionVoice); ok {
		t.Error("'### Voice' must not match the 'Voice & Habits' section")
	}
}

func TestReplaceSection(t *testing.T) {
	updated, err := ReplaceSection(testSpec, SectionVoice, "Fast cadence, warm wit.")
	if err != nil {
		t.Fatalf("ReplaceSection failed: %v", err)
	}
	body, ok := ExtractSection(updated, SectionVoice)
	if !ok || body != "Fast cadence, warm wit." {
		t.Errorf("Voice body after replace = %q (ok=%v)", body, ok)
	}

	// Neighbors are preserved.
	for _, section := range []string{SectionIdentity, SectionAppearance, SectionDialogue} {
		if _, ok := ExtractSection(updated, section); !ok {
			t.Errorf("section %s lost after replacing a neighbor", section)
		}
	}

	// Spacing normalizes to a single blank line between sections.
	if strings.Contains(updated, "\n\n\n") {
		t.Errorf("unexpected blank-line runs after replace:\n%s", updated)
	}

	// Appending a missing section.
	withScenario, err := ReplaceSection(testSpec, SectionScenario, "A dark forest, 1920.")
	if err != nil {
		t.Fatalf("ReplaceSection (append) failed: %v", err)
	}
	body, ok = ExtractSection(withScenario, SectionScenario)
	if !ok || body != "A dark forest, 1920." {
		t.Errorf("appended Scenario body = %q (ok=%v)", body, ok)
	}
	if !strings.HasSuffix(withScenario, "### Scenario\nA dark forest, 1920.\n") {
		t.Errorf("appended section should be last:\n%s", withScenario)
	}

	// Replacing the first section keeps the rest.
	updated, err = ReplaceSection(testSpec, SectionIdentity, "New identity.")
	if err != nil {
		t.Fatalf("ReplaceSection (first) failed: %v", err)
	}
	if body, ok := ExtractSection(updated, SectionIdentity); !ok || body != "New identity." {
		t.Errorf("first section body = %q (ok=%v)", body, ok)
	}
	if _, ok := ExtractSection(updated, SectionDialogue); !ok {
		t.Error("last section lost after replacing the first")
	}

	if _, err := ReplaceSection(testSpec, SectionVoice, "   "); err == nil {
		t.Error("expected empty body to be rejected")
	}
}

func TestRemoveSection(t *testing.T) {
	// Middle section: neighbors joined with a single blank line.
	stripped := RemoveSection(testSpec, SectionAppearance)
	if strings.Contains(stripped, "### Appearance") {
		t.Errorf("Appearance not removed:\n%s", stripped)
	}
	if !strings.Contains(stripped, "### Identity & Temperament") || !strings.Contains(stripped, "### Role & Relationships") {
		t.Errorf("neighbors lost after removal:\n%s", stripped)
	}
	if !strings.Contains(stripped, "in exile.\n\n### Role & Relationships") {
		t.Errorf("spacing not normalized after removal:\n%s", stripped)
	}

	// Last section.
	stripped = RemoveSection(testSpec, SectionDialogue)
	if strings.Contains(stripped, "### Example Dialogue") {
		t.Errorf("last section not removed:\n%s", stripped)
	}
	if !strings.HasSuffix(stripped, "dry wit.") {
		t.Errorf("spec should end at the preceding section:\n%s", stripped)
	}

	// Missing section: unchanged.
	if got := RemoveSection(testSpec, "Personality"); got != testSpec {
		t.Errorf("missing section removal changed the spec:\n%s", got)
	}
}

func TestStripScenarioSection(t *testing.T) {
	spec := "### Identity & Temperament\nBody.\n\n### Scenario\nScenario: A dark forest\nSome state.\n\n### Example Dialogue\n<START>\n"
	stripped := stripScenarioSection(spec)
	if strings.Contains(stripped, "### Scenario") {
		t.Errorf("Scenario not stripped:\n%s", stripped)
	}
	if !strings.Contains(stripped, "Body.\n\n### Example Dialogue") {
		t.Errorf("surrounding sections not preserved:\n%s", stripped)
	}

	// A scenario at the very start of the spec is stripped too.
	spec = "### Scenario\nState.\n\n### Identity & Temperament\nBody.\n"
	if stripped := stripScenarioSection(spec); strings.Contains(stripped, "### Scenario") {
		t.Errorf("leading Scenario not stripped:\n%s", stripped)
	}

	// No scenario: unchanged.
	if got := stripScenarioSection(testSpec); got != testSpec {
		t.Errorf("spec without Scenario changed:\n%s", got)
	}
}

func TestPersonaSectionOrder_RolePlacement(t *testing.T) {
	indexOf := func(name string) int {
		for i, s := range PersonaSectionOrder {
			if s == name {
				return i
			}
		}
		return -1
	}
	if !(indexOf(SectionAppearance) < indexOf(SectionRole) && indexOf(SectionRole) < indexOf(SectionVoice)) {
		t.Fatalf("Role & Relationships must sit between Appearance and Voice & Habits, got %v", PersonaSectionOrder)
	}

	body, ok := ExtractSection(testSpec, SectionRole)
	if !ok {
		t.Fatal("expected Role & Relationships section in testSpec")
	}
	if want := "- **Role/Occupation**: Cartographer"; body != want {
		t.Errorf("Role body = %q, want %q", body, want)
	}
}

func TestSplitSections(t *testing.T) {
	spec := "Preamble text.\n\n### Identity & Temperament\nBrash and brave.\n\n### Appearance\n- **Species**: Human"
	got := SplitSections(spec)
	want := []SpecSection{
		{Name: "", Body: "Preamble text."},
		{Name: "Identity & Temperament", Body: "Brash and brave."},
		{Name: "Appearance", Body: "- **Species**: Human"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sections, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("section %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGreetingSectionRoundTrip(t *testing.T) {
	spec := "### Identity & Temperament\nBody.\n\n### Greeting\nHey, I'm here.\n\n### Appearance\n- **Species**: Human"
	body, ok := ExtractSection(spec, SectionGreeting)
	if !ok || body != "Hey, I'm here." {
		t.Errorf("greeting = %q (ok=%v)", body, ok)
	}

	updated, err := ReplaceSection(spec, SectionGreeting, "Back.")
	if err != nil {
		t.Fatalf("ReplaceSection (greeting) failed: %v", err)
	}
	if body, ok := ExtractSection(updated, SectionGreeting); !ok || body != "Back." {
		t.Errorf("greeting after replace = %q (ok=%v)", body, ok)
	}
	// Neighbors are preserved.
	for _, section := range []string{SectionIdentity, SectionAppearance} {
		if _, ok := ExtractSection(updated, section); !ok {
			t.Errorf("section %s lost after editing the greeting", section)
		}
	}
}

func TestSplitSections_NoHeaders(t *testing.T) {
	got := SplitSections("Just a blob of text.")
	if len(got) != 1 || got[0].Name != "" || got[0].Body != "Just a blob of text." {
		t.Errorf("unexpected sections: %+v", got)
	}
}

func TestSplitSections_Empty(t *testing.T) {
	if got := SplitSections(""); len(got) != 0 {
		t.Errorf("expected no sections for empty spec, got %+v", got)
	}
	if got := SplitSections("\n\n"); len(got) != 0 {
		t.Errorf("expected no sections for blank spec, got %+v", got)
	}
}
