// Package llm talks to an OpenAI-compatible chat-completions endpoint. The
// LLM is used ONLY to humanize already-generated, deterministic test files —
// rewriting test titles, step descriptions, and trailing comments. The
// deterministic output is always retained as the fallback.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spriteCloud/quail-core/composer"
	"github.com/spriteCloud/quail-review/internal/config"
	"github.com/spriteCloud/quail-review/internal/log"
)

type Client struct {
	cfg          config.Config
	http         *http.Client
	announceOnce sync.Once
}

func New(cfg config.Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.LLMTimeout}}
}

// Ping issues a short GET to {OPENAI_BASE_URL}/models to confirm the
// chat endpoint is actually reachable. Useful in CI where a misrouted
// self-hosted endpoint (wireguard down, wrong port, model not loaded)
// would otherwise only surface as the first Humanize call timing out
// after the full LLMTimeout. Returns true on reachable (HTTP 2xx),
// false otherwise, plus the response status / error string for logs.
//
// v0.96.0 — added to make self-hosted endpoints (DGX-via-Netbird in
// the demo) self-diagnose at startup.
func (c *Client) Ping(ctx context.Context) (bool, string) {
	if !c.Enabled() {
		return false, "client disabled (no OPENAI_API_KEY or no model)"
	}
	url := strings.TrimRight(c.cfg.OpenAIBaseURL, "/") + "/models"
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(pingCtx, http.MethodGet, url, nil)
	if err != nil {
		return false, err.Error()
	}
	if c.cfg.OpenAIAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.OpenAIAPIKey)
	}
	r, err := c.http.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer r.Body.Close()
	if r.StatusCode/100 == 2 {
		return true, fmt.Sprintf("HTTP %d", r.StatusCode)
	}
	return false, fmt.Sprintf("HTTP %d", r.StatusCode)
}

func (c *Client) Enabled() bool {
	return c.cfg.OpenAIAPIKey != "" && c.cfg.Model != ""
}

type chatReq struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	MaxTokens int       `json:"max_tokens"`
}
type message struct{ Role, Content string }
type chatResp struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

// Humanize rewrites strings inside the deterministic file. It returns the
// modified content on success, or the original content unchanged on any
// error. The contract: same structure, only title/comment strings differ.
//
// v0.47 — honors QUAIL_HUMANIZE=0 to short-circuit this pass even
// when the LLM client is otherwise enabled. Useful when the composer
// (which also uses the LLM) is needed for scenario generation but
// humanizing every emitted spec is too slow for the run budget. The
// composer remains active in that mode.
func (c *Client) Humanize(ctx context.Context, lang, symbolName string, content []byte) []byte {
	if os.Getenv("QUAIL_HUMANIZE") == "0" {
		c.announceOnce.Do(func() {
			log.Info("llm humanization disabled (QUAIL_HUMANIZE=0); using deterministic output")
		})
		return content
	}
	if !c.Enabled() {
		c.announceOnce.Do(func() {
			log.Info("llm humanization disabled (no OPENAI_API_KEY); using deterministic output")
		})
		return content
	}
	c.announceOnce.Do(func() {
		log.Info("llm humanization enabled", "model", c.cfg.Model, "endpoint", c.cfg.OpenAIBaseURL)
	})
	prompt := buildPrompt(lang, symbolName, content)
	ctx, cancel := context.WithTimeout(ctx, c.cfg.LLMTimeout)
	defer cancel()
	resp, err := c.complete(ctx, prompt)
	if err != nil {
		log.Warn("llm humanize failed; falling back to deterministic", "err", err, "symbol", symbolName, "lang", lang)
		return content
	}
	humanized := applyRewrites(content, parseRewrites(resp))
	if !structurePreserved(lang, content, humanized) {
		log.Warn("llm output failed structure check; falling back to deterministic", "symbol", symbolName, "lang", lang)
		return content
	}
	// v0.92: .feature files have no `import|describe|it(|test(` shape,
	// so structurePreserved is a no-op for Gherkin. Without a Gherkin-
	// specific guard the LLM can rewrite a step value to include nested
	// unescaped quotes — Gherkin then parses the step with the wrong
	// {string} parameter count, no step-def binds, and bddgen aborts
	// with "Missing step definitions". Guard: every Given/When/Then in
	// the humanized output must still match a registered step-def
	// pattern.
	if composer.LooksLikeFeatureFile(humanized) && !composer.IsGherkinSafe(humanized) {
		log.Warn("llm humanize produced unrunnable Gherkin; falling back to deterministic", "symbol", symbolName)
		return content
	}
	log.Debug("llm humanize applied", "symbol", symbolName, "lang", lang)
	return humanized
}

// Chat is a public wrapper around the chat-completions call. The
// scenario composer (internal/composer) uses it directly with its own
// system prompt rather than going through Humanize. Returns the
// model's raw response text, or an error when the request fails / the
// client is disabled.
func (c *Client) Chat(ctx context.Context, systemMsg, userMsg string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("llm: client disabled (no OPENAI_API_KEY or no model)")
	}
	body, _ := json.Marshal(chatReq{
		Model:     c.cfg.Model,
		MaxTokens: c.cfg.LLMTokenCap,
		Messages: []message{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: userMsg},
		},
	})
	url := strings.TrimRight(c.cfg.OpenAIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.OpenAIAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.OpenAIAPIKey)
	}
	r, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if r.StatusCode/100 != 2 {
		return "", fmt.Errorf("llm: status %d: %s", r.StatusCode, raw)
	}
	var resp chatResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("llm: no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

func (c *Client) complete(ctx context.Context, userPrompt string) (string, error) {
	body, _ := json.Marshal(chatReq{
		Model:     c.cfg.Model,
		MaxTokens: c.cfg.LLMTokenCap,
		Messages: []message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	})
	url := strings.TrimRight(c.cfg.OpenAIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.OpenAIAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.OpenAIAPIKey)
	}
	r, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if r.StatusCode/100 != 2 {
		return "", fmt.Errorf("llm: status %d: %s", r.StatusCode, raw)
	}
	var resp chatResp
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("llm: no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

const systemPrompt = `You are a senior QA engineer rewriting test scaffolds so they read naturally to non-technical reviewers.
Rules:
- Only rewrite strings inside test titles (it/describe/test/test_X function name suffix) and trailing comments.
- Do NOT change identifiers, imports, assertions, control flow, indentation, or any code structure.
- Do NOT add or remove tests.
- Output STRICTLY a JSON object: {"rewrites": [{"from": "...", "to": "..."}]}
- "from" must match EXACTLY a substring already present in the input.
- Keep rewrites concise; clear human English.`

func buildPrompt(lang, symbol string, content []byte) string {
	return fmt.Sprintf("Language: %s\nSymbol under test: %s\n\nInput test file:\n---\n%s\n---\n\nReturn the JSON rewrites only.", lang, symbol, content)
}

type rewrite struct{ From, To string }

func parseRewrites(s string) []rewrite {
	// tolerate fenced or prose-prefixed responses
	if i := strings.Index(s, "{"); i > 0 {
		s = s[i:]
	}
	if j := strings.LastIndex(s, "}"); j >= 0 {
		s = s[:j+1]
	}
	var doc struct {
		Rewrites []rewrite `json:"rewrites"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		return nil
	}
	return doc.Rewrites
}

func applyRewrites(content []byte, rs []rewrite) []byte {
	out := string(content)
	for _, r := range rs {
		if r.From == "" || r.From == r.To {
			continue
		}
		if !strings.Contains(out, r.From) {
			continue
		}
		out = strings.ReplaceAll(out, r.From, r.To)
	}
	return []byte(out)
}

// structurePreserved is a coarse guard: the humanized file must have the same
// number of test-defining lines and the same import lines as the original.
func structurePreserved(lang string, before, after []byte) bool {
	if len(after) == 0 {
		return false
	}
	a, b := keyLines(lang, before), keyLines(lang, after)
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

var (
	reTestKeyword = regexp.MustCompile(`\b(import|require|describe|it\(|test\(|def test_|@Test|func Test)`)
)

func keyLines(_ string, b []byte) []string {
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(l)
		if reTestKeyword.MatchString(t) {
			// strip text inside the first string literal (titles can differ)
			out = append(out, stripLiteral(t))
		}
	}
	return out
}

var (
	reSingle = regexp.MustCompile(`'[^']*'`)
	reDouble = regexp.MustCompile(`"[^"]*"`)
	reBack   = regexp.MustCompile("`[^`]*`")
)

func stripLiteral(s string) string {
	s = reSingle.ReplaceAllString(s, `"_"`)
	s = reDouble.ReplaceAllString(s, `"_"`)
	s = reBack.ReplaceAllString(s, `"_"`)
	return s
}

func (c *Client) HumanizeAll(ctx context.Context, files map[string][]byte, symbols map[string]string, lang func(path string) string) map[string][]byte {
	out := make(map[string][]byte, len(files))
	deadline := time.Now().Add(c.cfg.LLMTimeout * time.Duration(len(files)+1))
	for path, body := range files {
		if time.Now().After(deadline) {
			out[path] = body
			continue
		}
		out[path] = c.Humanize(ctx, lang(path), symbols[path], body)
	}
	return out
}
