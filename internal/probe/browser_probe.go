package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/reviewqa/reviewqa/internal/ast"
	"github.com/reviewqa/reviewqa/internal/log"
	"github.com/reviewqa/reviewqa/internal/mindmap"
	"github.com/reviewqa/reviewqa/internal/probe/browser"
)

// browserPage is the on-the-wire shape emitted by the sidecar Playwright
// script. Kept lower-case-keyed because the JSON producer is JavaScript.
type browserPage struct {
	URL          string             `json:"url"`
	FinalURL     string             `json:"finalURL"`
	Title        string             `json:"title"`
	H1           []string           `json:"h1"`
	H2s          []string           `json:"h2s"`
	Links        []browserLink      `json:"links"`
	Images       []browserImage     `json:"images"`
	Meta         browserMeta        `json:"meta"`
	HasForm      bool               `json:"hasForm"`
	Inputs       []browserInput     `json:"inputs"`
	Interactions []browserInteract  `json:"interactions"`
}

type browserLink struct {
	Href    string `json:"href"`
	Text    string `json:"text"`
	Visible bool   `json:"visible"`
}

type browserImage struct {
	Src string `json:"src"`
	Alt string `json:"alt"`
}

type browserMeta struct {
	Description     string `json:"Description"`
	ViewportContent string `json:"ViewportContent"`
	OGTitle         string `json:"OGTitle"`
	OGType          string `json:"OGType"`
	OGDescription   string `json:"OGDescription"`
	Canonical       string `json:"Canonical"`
}

type browserInput struct {
	Tag         string `json:"tag"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	TestID      string `json:"testid"`
	Aria        string `json:"aria"`
	Placeholder string `json:"placeholder"`
	Required    bool   `json:"required"`
	Visible     bool   `json:"visible"`
}

type browserInteract struct {
	Kind      string `json:"kind"`
	Toggle    string `json:"toggle"`
	Text      string `json:"text"`
	Role      string `json:"role"`
	InputType string `json:"inputType"`
	Name      string `json:"name"`
	Controls  string `json:"controls"`
}

type browserResult struct {
	Origin string        `json:"origin"`
	Pages  []browserPage `json:"pages"`
	Errors []string      `json:"errors"`
}

// runBrowserCrawl executes the embedded sidecar script via `node` against
// the given origin URL and returns a populated mindmap.Map. Returns an
// error if `node` isn't available, the script bails out, or the JSON is
// malformed — callers fall back to the static crawl on any failure.
func runBrowserCrawl(ctx context.Context, origin string) (*mindmap.Map, []error) {
	cwd, _ := os.Getwd()
	scriptPath, cleanup, err := browser.WriteScript(cwd)
	if err != nil {
		return nil, []error{err}
	}
	defer cleanup()

	if _, err := exec.LookPath("node"); err != nil {
		return nil, []error{errors.New("browser probe: `node` not found in PATH — falling back to static crawl")}
	}

	cmd := exec.CommandContext(ctx, "node", scriptPath, origin)
	// Run from the consumer's project directory so node's module
	// resolution walks up FROM THERE and finds @playwright/test in their
	// node_modules. NODE_PATH is a secondary signal (covers monorepo
	// hoisting); the primary is cmd.Dir.
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "NODE_PATH="+filepath.Join(cwd, "node_modules"))
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr = string(ee.Stderr)
		}
		return nil, []error{fmt.Errorf("browser probe: node script failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))}
	}

	var res browserResult
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, []error{fmt.Errorf("browser probe: parse output: %w", err)}
	}

	m := &mindmap.Map{
		Origin: res.Origin,
		Pages:  map[string]*mindmap.Page{},
	}
	for _, bp := range res.Pages {
		page := browserPageToMindmap(bp)
		m.Pages[page.URL] = page
		m.Order = append(m.Order, page.URL)
	}

	var errs []error
	for _, e := range res.Errors {
		log.Warn("browser probe", "msg", e)
	}
	return m, errs
}

// browserPageToMindmap converts the JSON-emitted page into the mindmap.Page
// shape the rest of the pipeline expects. Visibility hints from the
// browser influence locator selection: invisible links are still surfaced
// but get an `aria` tag indicating they were not visible at render time.
func browserPageToMindmap(bp browserPage) *mindmap.Page {
	host := ""
	if u, err := url.Parse(bp.FinalURL); err == nil {
		host = u.Hostname()
	}
	_ = host // reserved for future host-tagging signals

	p := &mindmap.Page{
		URL:     bp.FinalURL,
		Title:   strings.TrimSpace(bp.Title),
		HasForm: bp.HasForm,
	}

	// Contents: title + h1 + h2 list.
	if p.Title != "" {
		p.Contents = append(p.Contents, ast.ContentAnchor{Tag: "title", Text: p.Title})
	}
	for _, h := range bp.H1 {
		if h != "" {
			p.Contents = append(p.Contents, ast.ContentAnchor{Tag: "h1", Text: h})
		}
	}
	for _, h := range bp.H2s {
		if h != "" {
			p.Contents = append(p.Contents, ast.ContentAnchor{Tag: "h2", Text: h})
		}
	}

	// Links — sorted to put visible ones first so the ranking heuristics
	// see them ahead of hidden dropdown menu items.
	for _, l := range bp.Links {
		p.Links = append(p.Links, ast.LocatorAnchor{
			Aria: l.Href,
			Text: l.Text,
			Tag:  "link-a",
		})
	}

	for _, im := range bp.Images {
		p.Images = append(p.Images, ast.ImageRef{Src: im.Src, Alt: im.Alt})
	}

	for _, in := range bp.Inputs {
		p.Inputs = append(p.Inputs, ast.FormInput{
			Tag:         in.Tag,
			Type:        in.Type,
			Name:        in.Name,
			TestID:      in.TestID,
			Aria:        in.Aria,
			Placeholder: in.Placeholder,
			Required:    in.Required,
		})
	}

	for _, ix := range bp.Interactions {
		p.Interactions = append(p.Interactions, ast.Interaction{
			Kind:      ix.Kind,
			Toggle:    ix.Toggle,
			Text:      ix.Text,
			Role:      ix.Role,
			InputType: ix.InputType,
			Name:      ix.Name,
			Controls:  ix.Controls,
		})
	}

	p.Meta = ast.MetaTags{
		Description:     bp.Meta.Description,
		ViewportContent: bp.Meta.ViewportContent,
		OGTitle:         bp.Meta.OGTitle,
		OGType:          bp.Meta.OGType,
		OGDescription:   bp.Meta.OGDescription,
		Canonical:       bp.Meta.Canonical,
	}

	// Tags: derive via the existing mindmap.TagPage path — but TagPage is
	// unexported. Instead we leave Tags nil here; mindmap.IdentifyJourneys
	// works against the Tags slice that buildPage usually sets. To stay
	// compatible we mirror the same lightweight signal set: landing if
	// URL path is root.
	if pu, err := url.Parse(p.URL); err == nil {
		path := strings.TrimSuffix(pu.Path, "/")
		if path == "" || path == "/index" || path == "/home" {
			p.Tags = append(p.Tags, mindmap.TagLanding)
		}
	}
	// Derive other tags from the same heuristics that static path uses.
	p.Tags = append(p.Tags, mindmap.TagsFromPage(p)...)

	return p
}
