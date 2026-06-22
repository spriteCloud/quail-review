// Package prompt turns natural-language test-area requests into a
// structured Filter the probe + journey-identification layers can apply.
// Tonight's implementation is deterministic vocabulary mapping; a future
// LLM-augmented version can plug in via the same Filter interface.
package prompt

import (
	"strings"

	"github.com/spriteCloud/quail-review/internal/mindmap"
)

// Filter is what `quail prompt` produces: a set of journey kinds and
// page-keyword hints to narrow the generation pipeline to what the user
// asked about. Empty filters degrade to "include everything" so passing
// the zero value matches all journeys.
type Filter struct {
	// Keywords are the cleaned, lowercased tokens extracted from the
	// prompt text. Used to match against page URLs and titles.
	Keywords []string
	// JourneyKinds are the mindmap.JourneyKind values the prompt
	// vocabulary maps to.
	JourneyKinds []mindmap.JourneyKind
	// PathHints are explicit URL paths the user mentioned in the prompt
	// (e.g. "the /signup page"). Always lowercase, leading-slash form.
	PathHints []string
	// Original is the raw input the user typed, retained for diagnostic
	// output ("parsed your prompt as ...").
	Original string
}

// keywordToKind maps human-language keywords to the journey kinds they
// most likely refer to. Order doesn't matter — Parse iterates the prompt
// tokens and accumulates all matching kinds.
var keywordToKind = map[string][]mindmap.JourneyKind{
	// purchase / convert family
	"checkout":       {mindmap.JourneyConvert},
	"cart":           {mindmap.JourneyConvert},
	"order":          {mindmap.JourneyConvert},
	"payment":        {mindmap.JourneyConvert},
	"pay":            {mindmap.JourneyConvert},
	"buy":            {mindmap.JourneyConvert},
	"subscribe":      {mindmap.JourneyConvert},
	"subscription":   {mindmap.JourneyConvert},
	"newsletter":     {mindmap.JourneyConvert},
	"demo":           {mindmap.JourneyConvert},
	// auth family
	"signup":   {mindmap.JourneyAuthenticate, mindmap.JourneyConvert},
	"signin":   {mindmap.JourneyAuthenticate},
	"login":    {mindmap.JourneyAuthenticate},
	"register": {mindmap.JourneyAuthenticate},
	"auth":     {mindmap.JourneyAuthenticate},
	// contact family
	"contact": {mindmap.JourneyContact},
	"talk":    {mindmap.JourneyContact},
	"reach":   {mindmap.JourneyContact},
	"message": {mindmap.JourneyContact},
	// pricing / evaluation
	"pricing": {mindmap.JourneyEvaluate},
	"plans":   {mindmap.JourneyEvaluate},
	"plan":    {mindmap.JourneyEvaluate},
	"cost":    {mindmap.JourneyEvaluate},
	"tariff":  {mindmap.JourneyEvaluate},
	// search / in-page interactions
	"search": {mindmap.JourneyExercise},
	"find":   {mindmap.JourneyExercise},
	"lookup": {mindmap.JourneyExercise},
	"filter": {mindmap.JourneyExercise},
	// content / read
	"blog":    {mindmap.JourneyRead, mindmap.JourneyBrowse},
	"article": {mindmap.JourneyRead, mindmap.JourneyBrowse},
	"post":    {mindmap.JourneyRead, mindmap.JourneyBrowse},
	"guide":   {mindmap.JourneyRead, mindmap.JourneyBrowse},
	"read":    {mindmap.JourneyRead},
	"docs":    {mindmap.JourneyRead, mindmap.JourneyBrowse},
	"news":    {mindmap.JourneyRead, mindmap.JourneyBrowse},
	// case studies
	"case":     {mindmap.JourneyResearch},
	"study":    {mindmap.JourneyResearch},
	"customer": {mindmap.JourneyResearch},
	"success":  {mindmap.JourneyResearch},
	"story":    {mindmap.JourneyResearch},
	// services / products
	"product":  {mindmap.JourneyDiscover},
	"feature":  {mindmap.JourneyDiscover},
	"service":  {mindmap.JourneyDiscover},
	"solution": {mindmap.JourneyDiscover},
	"offering": {mindmap.JourneyDiscover},
	// generic explore
	"about":      {mindmap.JourneyExplore},
	"team":       {mindmap.JourneyExplore},
	"company":    {mindmap.JourneyExplore},
	"who":        {mindmap.JourneyExplore},
	"explore":    {mindmap.JourneyExplore},
	"navigate":   {mindmap.JourneyExplore},
	"navigation": {mindmap.JourneyExplore},
}

// stopWords are tokens we strip before keyword matching — common
// articles and verbs that carry no signal.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "to": true, "for": true,
	"and": true, "or": true, "but": true, "in": true, "on": true, "at": true,
	"with": true, "by": true, "from": true, "as": true, "is": true, "are": true,
	"was": true, "be": true, "been": true, "being": true, "have": true, "has": true,
	"had": true, "do": true, "does": true, "did": true, "doing": true,
	"i": true, "we": true, "you": true, "they": true, "this": true, "that": true,
	"want": true, "need": true, "test": true, "verify": true, "check": true,
	"validate": true, "make": true, "sure": true, "going": true, "would": true,
	"could": true, "should": true, "can": true, "will": true, "let": true,
	"users": true, "user": true, "page": true, "pages": true, "flow": true, "flows": true,
}

// Parse tokenises a natural-language prompt into a Filter. Empty/unknown
// prompts produce a Filter with Original set and empty other fields —
// the caller treats that as "no filter" and emits everything.
func Parse(text string) Filter {
	f := Filter{Original: text}
	if strings.TrimSpace(text) == "" {
		return f
	}
	lower := strings.ToLower(text)
	tokens := tokenize(lower)
	seenKinds := map[mindmap.JourneyKind]bool{}
	seenKeywords := map[string]bool{}
	for _, tok := range tokens {
		if stopWords[tok] {
			continue
		}
		// Explicit path hint — token starts with /.
		if strings.HasPrefix(tok, "/") && len(tok) > 1 {
			f.PathHints = append(f.PathHints, tok)
			continue
		}
		if kinds, ok := keywordToKind[tok]; ok {
			for _, k := range kinds {
				if !seenKinds[k] {
					seenKinds[k] = true
					f.JourneyKinds = append(f.JourneyKinds, k)
				}
			}
		}
		// Also try the singular form when the token ends in 's' — saves
		// us listing both "plan" and "plans" everywhere.
		if strings.HasSuffix(tok, "s") {
			if kinds, ok := keywordToKind[strings.TrimSuffix(tok, "s")]; ok {
				for _, k := range kinds {
					if !seenKinds[k] {
						seenKinds[k] = true
						f.JourneyKinds = append(f.JourneyKinds, k)
					}
				}
			}
		}
		if !seenKeywords[tok] && len(tok) >= 3 {
			seenKeywords[tok] = true
			f.Keywords = append(f.Keywords, tok)
		}
	}
	return f
}

// tokenize splits on whitespace + common punctuation while preserving
// leading-slash path hints. Single-character tokens are skipped.
func tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', ',', '.', ';', ':', '!', '?', '"', '\'', '(', ')', '[', ']':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// IsEmpty reports whether the filter would degrade to "include
// everything". Callers check this to decide whether to apply filtering
// or skip it.
func (f Filter) IsEmpty() bool {
	return len(f.Keywords) == 0 && len(f.JourneyKinds) == 0 && len(f.PathHints) == 0
}
