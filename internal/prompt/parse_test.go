package prompt

import (
	"testing"

	"github.com/spriteCloud/quail-review/internal/mindmap"
)

func TestParse_MapsKeywordsToKinds(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []mindmap.JourneyKind
		wantKw  []string
		wantPth []string
	}{
		{
			name:  "checkout flow",
			input: "test the checkout flow",
			want:  []mindmap.JourneyKind{mindmap.JourneyConvert},
		},
		{
			name:  "signup / authenticate",
			input: "verify the signup form",
			want:  []mindmap.JourneyKind{mindmap.JourneyAuthenticate, mindmap.JourneyConvert},
		},
		{
			name:  "explicit /login path hint",
			input: "exercise the /login page",
			want:  nil, // no keyword matched (login itself maps but "/login" goes to PathHints)
			wantPth: []string{"/login"},
		},
		{
			name:  "contact form invalid email",
			input: "verify the contact form rejects invalid emails",
			want:  []mindmap.JourneyKind{mindmap.JourneyContact},
		},
		{
			name:  "pricing page evaluation",
			input: "make sure the pricing page shows plans",
			want:  []mindmap.JourneyKind{mindmap.JourneyEvaluate},
		},
		{
			name:  "search exercise",
			input: "test that search works",
			want:  []mindmap.JourneyKind{mindmap.JourneyExercise},
		},
		{
			name:  "docs read",
			input: "explore the docs section",
			want:  []mindmap.JourneyKind{mindmap.JourneyExplore, mindmap.JourneyRead, mindmap.JourneyBrowse},
		},
		{
			name:  "case study research",
			input: "validate the case studies list",
			want:  []mindmap.JourneyKind{mindmap.JourneyResearch},
		},
		{
			name:  "service discover",
			input: "verify the services pages render",
			want:  []mindmap.JourneyKind{mindmap.JourneyDiscover},
		},
		{
			name:  "completely irrelevant prompt",
			input: "the quick brown fox jumps",
			want:  nil, // no recognised kinds — filter is empty for kinds
		},
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := Parse(tc.input)
			if !sameKinds(f.JourneyKinds, tc.want) {
				t.Errorf("kinds = %+v; want %+v", f.JourneyKinds, tc.want)
			}
			if tc.wantPth != nil && !sameStrings(f.PathHints, tc.wantPth) {
				t.Errorf("paths = %+v; want %+v", f.PathHints, tc.wantPth)
			}
		})
	}
}

func TestParse_KeywordsAreLowercaseAndDeduped(t *testing.T) {
	f := Parse("Pricing Pricing PRICING")
	if len(f.Keywords) != 1 || f.Keywords[0] != "pricing" {
		t.Errorf("expected one lowercase 'pricing'; got %+v", f.Keywords)
	}
}

func TestParse_IsEmptyOnNoSignal(t *testing.T) {
	if !Parse("").IsEmpty() {
		t.Error("empty input must produce empty filter")
	}
	if !Parse("the").IsEmpty() {
		t.Error("stop-words only must produce empty filter")
	}
	if Parse("checkout").IsEmpty() {
		t.Error("recognised keyword must NOT produce empty filter")
	}
}

func sameKinds(a, b []mindmap.JourneyKind) bool {
	if len(a) != len(b) {
		return false
	}
	got := map[mindmap.JourneyKind]bool{}
	for _, k := range a {
		got[k] = true
	}
	for _, k := range b {
		if !got[k] {
			return false
		}
	}
	return true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
