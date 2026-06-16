package plan

import (
	"testing"

	"reflect"
)

func TestExtractContentAnchors_DecodesHTMLEntities(t *testing.T) {
	html := []byte(`<html>
<title>Let&#x27;s Chat &amp; Talk</title>
<body>
<h1>Let&#x27;s Chat</h1>
<h2>Joe&apos;s &lt;Demo&gt;</h2>
</body>
</html>`)
	out := ExtractContentAnchors(html)

	got := map[string]string{}
	for _, c := range out {
		got[c.Tag] = c.Text
	}

	if got["title"] != "Let's Chat & Talk" {
		t.Errorf("title decode: got %q, want %q", got["title"], "Let's Chat & Talk")
	}
	if got["h1"] != "Let's Chat" {
		t.Errorf("h1 decode: got %q, want %q", got["h1"], "Let's Chat")
	}
	if got["h2"] != "Joe's <Demo>" {
		t.Errorf("h2 decode: got %q, want %q", got["h2"], "Joe's <Demo>")
	}
}

func TestPageTitle_DecodesHTMLEntities(t *testing.T) {
	html := []byte(`<title>Let&#x27;s Chat</title>`)
	if got := PageTitle(html); got != "Let's Chat" {
		t.Errorf("PageTitle: got %q, want %q", got, "Let's Chat")
	}
}
func TestPageTitle(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		got := PageTitle(nil)
		if reflect.DeepEqual(got, *new(string)) {
			t.Fatalf("got zero value: %#v", got)
		}
	})

	t.Run("returns expected type", func(t *testing.T) {
		got := PageTitle(nil)
		if got, want := reflect.TypeOf(got), reflect.TypeOf(*new(string)); got != want {
			t.Fatalf("type = %v, want %v", got, want)
		}
	})
}
