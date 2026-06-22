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

	"github.com/spriteCloud/quail-review/internal/ast"
	"github.com/spriteCloud/quail-review/internal/log"
	"github.com/spriteCloud/quail-review/internal/mindmap"
	"github.com/spriteCloud/quail-review/internal/plan"
	"github.com/spriteCloud/quail-review/internal/probe/browser"
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
	RoleAnchors  []browserRoleAnchor `json:"roleAnchors"`
	DOMHTML      string             `json:"domHTML"`
	Forms        []browserForm      `json:"forms"`
}

type browserRoleAnchor struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	AriaLabel string `json:"ariaLabel"`
	TestID    string `json:"testid"`
	Visible   bool   `json:"visible"`
}

type browserForm struct {
	Action  string         `json:"action"`
	Method  string         `json:"method"`
	EncType string         `json:"enctype"`
	Inputs  []browserInput `json:"inputs"`
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
// the given origin URL and returns a populated mindmap.Map. The engine
// (chromium|firefox|webkit) selects which Playwright runtime to launch,
// stealth toggles playwright-extra + the stealth plugin, and opts
// controls how wide/deep the BFS crawl goes. Returns an error if `node`
// isn't available, the runner can't be ensured for this engine, the
// script bails out, or the JSON is malformed.
func runBrowserCrawl(ctx context.Context, origin string, engine EngineMode, stealth bool, opts mindmap.Options) (*mindmap.Map, []error) {
	if _, err := exec.LookPath("node"); err != nil {
		return nil, []error{fmt.Errorf("%w: `node` not found in PATH", browser.ErrBrowserUnavailable)}
	}

	if engine == "" || engine == EngineAuto {
		engine = EngineChromium
	}

	runnerDir, err := browser.EnsureRunner(ctx, string(engine))
	if err != nil {
		return nil, []error{err}
	}

	scriptPath, cleanup, err := browser.WriteScript(runnerDir)
	if err != nil {
		return nil, []error{err}
	}
	defer cleanup()

	stealthVal := "on"
	if !stealth {
		stealthVal = "off"
	}
	// v0.90: thread coverage's page/depth budget into the browser
	// sidecar. Previously the sidecar hard-capped at 20 pages /
	// depth 3 regardless of --coverage, so `--coverage max` only
	// affected the static crawl. Defaults stay in probe.mjs for
	// backwards compat with direct node invocations.
	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = 20
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}
	log.Info("browser probe: launching", "engine", string(engine), "stealth", stealthVal, "maxPages", maxPages, "maxDepth", maxDepth, "origin", origin)

	cmd := exec.CommandContext(ctx, "node", scriptPath, origin)
	// Run from the shared runner so node's ESM resolver walks
	// <runner>/.quail-browser-probe-XXX → <runner> →
	// <runner>/node_modules — which EnsureRunner just populated.
	// Independent of where the probed project lives on disk.
	cmd.Dir = runnerDir
	cmd.Env = append(os.Environ(),
		"NODE_PATH="+filepath.Join(runnerDir, "node_modules"),
		"QUAIL_ENGINE="+string(engine),
		"QUAIL_STEALTH="+stealthVal,
		fmt.Sprintf("QUAIL_MAX_PAGES=%d", maxPages),
		fmt.Sprintf("QUAIL_MAX_DEPTH=%d", maxDepth),
	)
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
		DOMHTML: bp.DOMHTML,
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
	// v0.92: log interaction counts so SSE viewers can see at a
	// glance whether the broadened selectors caught anything. Quiet
	// when the page has none.
	if len(p.Interactions) > 0 {
		log.Debug("browser probe: interactions captured", "url", p.URL, "count", len(p.Interactions))
	}

	p.Meta = ast.MetaTags{
		Description:     bp.Meta.Description,
		ViewportContent: bp.Meta.ViewportContent,
		OGTitle:         bp.Meta.OGTitle,
		OGType:          bp.Meta.OGType,
		OGDescription:   bp.Meta.OGDescription,
		Canonical:       bp.Meta.Canonical,
	}

	for _, f := range bp.Forms {
		spec := ast.FormSpec{
			Action:  f.Action,
			Method:  strings.ToLower(f.Method),
			EncType: strings.ToLower(f.EncType),
		}
		for _, in := range f.Inputs {
			spec.Inputs = append(spec.Inputs, ast.FormInput{
				Tag:         in.Tag,
				Type:        in.Type,
				Name:        in.Name,
				TestID:      in.TestID,
				Aria:        in.Aria,
				Placeholder: in.Placeholder,
				Required:    in.Required,
			})
		}
		p.Forms = append(p.Forms, spec)
	}

	// v0.90: synthesise Anchors from the rendered DOM. The static
	// crawl path runs ExtractHTMLAnchors over the raw HTML and stores
	// the result on p.Anchors — that's what isFormPage looks at to
	// find a `submit`-tagged anchor before flagging a form journey.
	// The browser path was leaving p.Anchors nil, so form / contact /
	// auth journey detection silently never fired on browser-probed
	// pages (e.g. ing.nl's mortgage calculator). Run the same
	// extractor over DOMHTML now that we have it.
	if p.DOMHTML != "" {
		p.Anchors = plan.ExtractHTMLAnchors(p.URL, []byte(p.DOMHTML))
	}

	// v0.91: append Playwright-resolved role-tagged actionables.
	// The regex extractor above catches role="..." in the rendered
	// HTML, but some frameworks set role via JS after DOM emission
	// or use implicit ARIA roles that the regex misses. The
	// Playwright query in probe.mjs catches both. DedupAnchors
	// folds overlap with the regex output.
	for _, r := range bp.RoleAnchors {
		role := strings.ToLower(strings.TrimSpace(r.Role))
		anchorTag := ""
		switch role {
		case "button", "submit":
			anchorTag = "submit"
		case "link", "menuitem":
			anchorTag = "link-a"
		}
		p.Anchors = append(p.Anchors, ast.LocatorAnchor{
			TestID: r.TestID,
			Aria:   r.AriaLabel,
			Role:   role,
			Name:   r.Text,
			Tag:    anchorTag,
		})
	}
	p.Anchors = ast.DedupAnchors(p.Anchors)

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
