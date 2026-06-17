package patterns

import "testing"

func TestPatternRegistry_HiddenClass(t *testing.T) {
	tests := []struct {
		name string
		ctx  Context
		want Action
		fire string // expected matching pattern name; empty if no match
	}{
		{
			name: "aria-hidden true → drop",
			ctx:  Context{Tag: "div", Attrs: `aria-hidden="true" class="modal"`},
			want: ActionDrop, fire: "a11y_hidden",
		},
		{
			name: "bare hidden attribute → drop",
			ctx:  Context{Tag: "div", Attrs: `hidden class="modal"`},
			want: ActionDrop, fire: "a11y_hidden",
		},
		{
			name: "skip-to-main aria-label → drop",
			ctx:  Context{Tag: "a", Attrs: `href="#main" aria-label="Skip to main content"`},
			want: ActionDrop, fire: "a11y_hidden",
		},
		{
			name: "skip-nav aria-label → drop",
			ctx:  Context{Tag: "a", Attrs: `aria-label="Skip navigation"`},
			want: ActionDrop, fire: "a11y_hidden",
		},
		{
			name: "sr-only class → drop",
			ctx:  Context{Tag: "span", Attrs: `class="sr-only"`},
			want: ActionDrop, fire: "sr_only",
		},
		{
			name: "visually-hidden class → drop",
			ctx:  Context{Tag: "span", Attrs: `class="custom visually-hidden other"`},
			want: ActionDrop, fire: "sr_only",
		},
		{
			name: "Bootstrap d-none → drop",
			ctx:  Context{Tag: "div", Attrs: `class="d-none d-md-block"`},
			want: ActionDrop, fire: "bootstrap_hidden",
		},
		{
			name: "Bootstrap hidden-md-up → drop",
			ctx:  Context{Tag: "div", Attrs: `class="hidden-md-up"`},
			want: ActionDrop, fire: "bootstrap_hidden",
		},
		{
			name: "inline display:none → drop",
			ctx:  Context{Tag: "div", Attrs: `style="display: none; color: red"`},
			want: ActionDrop, fire: "inline_hidden",
		},
		{
			name: "inline visibility:hidden → drop",
			ctx:  Context{Tag: "div", Attrs: `style="visibility:hidden"`},
			want: ActionDrop, fire: "inline_hidden",
		},
		{
			name: "off-screen left:-9999px → drop",
			ctx:  Context{Tag: "div", Attrs: `style="position:absolute; left:-9999px"`},
			want: ActionDrop, fire: "inline_hidden",
		},
		{
			name: "input type=hidden → drop",
			ctx:  Context{Tag: "input", Attrs: `type="hidden" name="csrf" value="abc"`},
			want: ActionDrop, fire: "form_hidden",
		},
		{
			name: "input type=text NOT a form_hidden match",
			ctx:  Context{Tag: "input", Attrs: `type="text" name="email"`},
			want: ActionInclude, fire: "",
		},
		{
			name: "GTM noscript → drop",
			ctx:  Context{Tag: "noscript", Inner: `<iframe src="https://www.googletagmanager.com/ns.html?id=GTM-X"></iframe>`},
			want: ActionDrop, fire: "tracker",
		},
		{
			name: "FB pixel script → drop",
			ctx:  Context{Tag: "script", Attrs: `src="https://connect.facebook.net/en_US/fbevents.js"`},
			want: ActionDrop, fire: "tracker",
		},
		{
			name: "ordinary text element → include",
			ctx:  Context{Tag: "h1", Attrs: `class="hero-heading"`},
			want: ActionInclude, fire: "",
		},
		{
			name: "data-hidden-state attribute MUST NOT trigger a11y_hidden",
			ctx:  Context{Tag: "div", Attrs: `data-hidden-state="open"`},
			want: ActionInclude, fire: "",
		},
		{
			name: "print-only block → drop",
			ctx:  Context{Tag: "div", Attrs: `class="d-print-block d-none"`},
			want: ActionDrop,
			// Either print_only or bootstrap_hidden may catch it; the
			// effect (drop) is what matters. Don't pin fire name.
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.ctx)
			if got != tc.want {
				t.Errorf("Decide(%q) = %v; want %v", tc.ctx.Attrs, got, tc.want)
			}
			if tc.fire != "" {
				p := MatchingPattern(tc.ctx)
				if p == nil {
					t.Errorf("expected pattern %q to fire; got nil", tc.fire)
				} else if p.Name() != tc.fire {
					t.Errorf("expected pattern %q; got %q", tc.fire, p.Name())
				}
			}
		})
	}
}

func TestPatternRegistry_AllReturnsCopy(t *testing.T) {
	all := All()
	if len(all) < 7 {
		t.Fatalf("expected ≥7 registered patterns; got %d", len(all))
	}
	all[0] = nil
	if all2 := All(); all2[0] == nil {
		t.Error("All() must return a defensive copy; registry was mutated")
	}
}
