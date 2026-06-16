package ast

import "testing"

func TestDedupAnchors_CollapsesIdenticalRoleHits(t *testing.T) {
	anchors := []LocatorAnchor{
		{Role: "banner", Tag: "header"},
		{Role: "banner", Tag: "header"},
		{Role: "banner", Tag: "header"},
	}
	got := DedupAnchors(anchors)
	if len(got) != 1 {
		t.Fatalf("expected 1 anchor after dedup, got %d: %+v", len(got), got)
	}
}

func TestDedupAnchors_KeepsDistinctTestIDs(t *testing.T) {
	anchors := []LocatorAnchor{
		{TestID: "hero", Tag: "div"},
		{TestID: "footer", Tag: "div"},
	}
	got := DedupAnchors(anchors)
	if len(got) != 2 {
		t.Errorf("distinct testids should not be merged: %+v", got)
	}
}

func TestDedupAnchors_LinkAnchorsPassThrough(t *testing.T) {
	anchors := []LocatorAnchor{
		{Aria: "/about", Tag: "link-a"},
		{Aria: "/contact", Tag: "link-a"},
		{Aria: "/about", Tag: "link-a"}, // dup but passes the dedup boundary; DedupLinks handles it.
	}
	got := DedupAnchors(anchors)
	if len(got) != 3 {
		t.Errorf("DedupAnchors should not collapse link-shaped anchors (DedupLinks does): %+v", got)
	}
}

func TestDedupAnchors_CapsAtMaxAnchors(t *testing.T) {
	var anchors []LocatorAnchor
	for i := 0; i < 20; i++ {
		anchors = append(anchors, LocatorAnchor{TestID: string(rune('a' + i)), Tag: "div"})
	}
	got := DedupAnchors(anchors)
	if len(got) != 8 {
		t.Errorf("expected cap of 8, got %d", len(got))
	}
}

func TestDedupInputs(t *testing.T) {
	in := []FormInput{
		{Name: "email", Type: "email"},
		{Name: "email", Type: "email"}, // dup
		{Name: "password", Type: "password"},
	}
	got := DedupInputs(in)
	if len(got) != 2 {
		t.Errorf("expected 2 deduped inputs, got %d: %+v", len(got), got)
	}
}

func TestDedupLinks(t *testing.T) {
	in := []LocatorAnchor{
		{Aria: "/about", Tag: "link-a"},
		{Aria: "/about", Tag: "link-a"},
		{Aria: "/contact", Tag: "link-a"},
	}
	got := DedupLinks(in)
	if len(got) != 2 {
		t.Errorf("expected 2 deduped links, got %d", len(got))
	}
}
