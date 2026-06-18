package plan

import (
	"strings"
	"testing"
)

// v0.91: role="button" / role="submit" on a non-button tag must
// surface as an Anchor with Tag="submit" so isFormPage's hasSubmit
// check finds it. Same for role="link"/menuitem mapping to link-a.
func TestExtractHTMLAnchors_RoleButtonGetsSubmitTag(t *testing.T) {
	html := `<form><input required name="email"/>
<div role="button">Send</div>
</form>`
	anchors := ExtractHTMLAnchors("test.html", []byte(html))
	if len(anchors) == 0 {
		t.Fatal("expected anchors for role=button; got none")
	}
	var found bool
	for _, a := range anchors {
		if strings.EqualFold(a.Role, "button") && a.Tag == "submit" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected anchor with Role=button + Tag=submit; got %+v", anchors)
	}
}

func TestExtractHTMLAnchors_RoleLinkGetsLinkATag(t *testing.T) {
	html := `<span role="link">Read more</span>`
	anchors := ExtractHTMLAnchors("test.html", []byte(html))
	var found bool
	for _, a := range anchors {
		if strings.EqualFold(a.Role, "link") && a.Tag == "link-a" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected anchor with Role=link + Tag=link-a; got %+v", anchors)
	}
}

// Non-action roles (tab, dialog, navigation) keep the lowercased
// HTML tag — we only promote the click-shaped roles to submit/link.
func TestExtractHTMLAnchors_NonActionRoleKeepsTag(t *testing.T) {
	html := `<div role="tab">Settings</div>`
	anchors := ExtractHTMLAnchors("test.html", []byte(html))
	for _, a := range anchors {
		if strings.EqualFold(a.Role, "tab") {
			if a.Tag == "submit" || a.Tag == "link-a" {
				t.Errorf("role=tab should keep tag, not be promoted: %+v", a)
			}
		}
	}
}
