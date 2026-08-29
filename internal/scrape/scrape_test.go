package scrape

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"characterllm/internal/safehttp"
)

const fixtureHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Test Character - Wiki</title>
  <script>var x = "should not appear";</script>
  <style>.a { color: red; }</style>
</head>
<body>
  <nav>Home About Contact</nav>
  <header>Site banner</header>
  <main>
    <h1>Test Character</h1>
    <p>First paragraph about the character.</p>
    <div>Second block with <b>bold</b> text.</div>
    <ul><li>Item one</li><li>Item two</li></ul>
    <script>var y = "also hidden";</script>
    <p>Final paragraph.</p>
  </main>
  <footer>Copyright notice</footer>
</body>
</html>`

func newTestScraper(t *testing.T, handler http.HandlerFunc) ScrapeSource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	// Bypass the https/public-IP policy: pin the fetch to the test server.
	f := &safehttp.Fetcher{
		Validate: func(ctx context.Context, raw string) (string, string, error) {
			return srv.URL, "localhost", nil
		},
	}
	return New(f)
}

func TestScrape_HTML(t *testing.T) {
	s := newTestScraper(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(fixtureHTML))
	})

	src, err := s.Scrape(context.Background(), "https://example.com/page")
	if err != nil {
		t.Fatalf("Scrape failed: %v", err)
	}
	if src.Title != "Test Character - Wiki" {
		t.Errorf("unexpected title: %q", src.Title)
	}
	for _, want := range []string{
		"Test Character",
		"First paragraph about the character.",
		"Second block with bold text.",
		"Item one",
		"Item two",
		"Final paragraph.",
	} {
		if !strings.Contains(src.Text, want) {
			t.Errorf("text missing %q:\n%s", want, src.Text)
		}
	}
	for _, junk := range []string{
		"should not appear", "also hidden", "color: red",
		"Home About Contact", "Site banner", "Copyright notice",
	} {
		if strings.Contains(src.Text, junk) {
			t.Errorf("text must not contain %q:\n%s", junk, src.Text)
		}
	}
}

func TestScrape_PlainText(t *testing.T) {
	s := newTestScraper(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("  just some text  \n"))
	})

	src, err := s.Scrape(context.Background(), "https://example.com/plain")
	if err != nil {
		t.Fatalf("Scrape failed: %v", err)
	}
	if src.Text != "just some text" {
		t.Errorf("unexpected text: %q", src.Text)
	}
	if src.Title != "" {
		t.Errorf("plain text must have no title, got %q", src.Title)
	}
}

func TestScrape_UnsupportedContentType(t *testing.T) {
	s := newTestScraper(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4"))
	})
	if _, err := s.Scrape(context.Background(), "https://example.com/pdf"); err == nil {
		t.Error("expected error for unsupported content type")
	}
}

func TestScrape_EmptyExtraction(t *testing.T) {
	s := newTestScraper(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Empty</title></head><body><nav>Only nav</nav></body></html>`))
	})
	if _, err := s.Scrape(context.Background(), "https://example.com/empty"); err == nil {
		t.Error("expected error when no readable text is extracted")
	}
}

func TestScrape_HTTPError(t *testing.T) {
	s := newTestScraper(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := s.Scrape(context.Background(), "https://example.com/403"); err == nil {
		t.Error("expected error for non-200 response")
	}
}

func TestScrape_OversizedBody(t *testing.T) {
	s := newTestScraper(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><p>Start of a huge page. </p>"))
		for i := 0; i < maxBodyBytes; i++ {
			w.Write([]byte("x"))
		}
	})
	src, err := s.Scrape(context.Background(), "https://example.com/huge")
	if err != nil {
		t.Fatalf("Scrape failed: %v", err)
	}
	if !strings.Contains(src.Text, "Start of a huge page") {
		t.Errorf("expected the readable head of the page, got %q", src.Text[:min(len(src.Text), 80)])
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a\n\n\n\nb", "a\n\nb"},
		{"  padded  \n\n\nend  ", "padded\n\nend"},
		{"single", "single"},
	}
	for _, tt := range tests {
		if got := cleanText(tt.in); got != tt.want {
			t.Errorf("cleanText(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}
