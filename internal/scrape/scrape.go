// Package scrape downloads web pages and extracts their readable text for
// character research.
package scrape

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"characterllm/internal/safehttp"
)

// maxBodyBytes caps how much of a page body is read before parsing.
const maxBodyBytes = 2 << 20

// Source is the extracted content of a fetched page.
type Source struct {
	URL   string
	Title string
	Text  string
}

// ScrapeSource fetches a URL and extracts its readable content.
type ScrapeSource interface {
	Scrape(ctx context.Context, url string) (Source, error)
}

type scraper struct {
	fetcher *safehttp.Fetcher
}

// New returns a ScrapeSource that downloads pages through the given fetcher
// (the built-in safehttp policy when nil) and converts them to plain text.
func New(f *safehttp.Fetcher) ScrapeSource {
	if f == nil {
		f = safehttp.NewFetcher()
	}
	return &scraper{fetcher: f}
}

// Scrape fetches the URL and returns its title and readable text.
func (s *scraper) Scrape(ctx context.Context, raw string) (Source, error) {
	resp, err := s.fetcher.Get(ctx, raw)
	if err != nil {
		return Source{}, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Source{}, fmt.Errorf("reading body failed: %w", err)
	}

	switch contentType(resp.Header.Get("Content-Type")) {
	case "text/plain":
		text := strings.TrimSpace(string(body))
		if text == "" {
			return Source{}, fmt.Errorf("no readable text in response")
		}
		return Source{URL: raw, Text: text}, nil
	case "":
		// No usable Content-Type: treat the body as HTML.
	case "text/html", "application/xhtml+xml":
		// parsed below
	default:
		return Source{}, fmt.Errorf("unsupported content type")
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return Source{}, fmt.Errorf("html parse failed: %w", err)
	}

	var title, text strings.Builder
	walk(doc, &title, &text)
	cleaned := cleanText(text.String())
	if cleaned == "" {
		return Source{}, fmt.Errorf("no readable text extracted")
	}
	return Source{URL: raw, Title: strings.TrimSpace(title.String()), Text: cleaned}, nil
}

func contentType(header string) string {
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	return mediaType
}

// skipTags are subtrees that never carry page content.
var skipTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true,
	"iframe": true, "svg": true, "form": true, "nav": true, "header": true,
	"footer": true, "aside": true,
}

// blockTags delimit paragraphs: closing one ends the current text block.
var blockTags = map[string]bool{
	"p": true, "div": true, "li": true, "tr": true, "table": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"br": true, "hr": true, "article": true, "section": true,
	"blockquote": true, "pre": true, "ul": true, "ol": true,
	"dd": true, "dt": true, "figcaption": true,
}

func walk(n *html.Node, title, text *strings.Builder) {
	switch n.Type {
	case html.ElementNode:
		tag := n.Data
		if tag == "title" {
			if n.FirstChild != nil {
				title.WriteString(n.FirstChild.Data)
			}
			return
		}
		if skipTags[tag] {
			return
		}
		walkChildren(n, title, text)
		if blockTags[tag] {
			text.WriteString("\n\n")
		}
	case html.TextNode:
		// The leading space separates adjacent text chunks ("with" + <b>bold</b>);
		// cleanText collapses it against the block breaks it may butt up on.
		if words := strings.Join(strings.Fields(n.Data), " "); words != "" {
			text.WriteString(" " + words)
		}
	default:
		// Document nodes and other wrappers: recurse without markup.
		walkChildren(n, title, text)
	}
}

func walkChildren(n *html.Node, title, text *strings.Builder) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, title, text)
	}
}

var blankRun = regexp.MustCompile(`\n{3,}`)

// cleanText normalizes the extracted text: trims lines and collapses runs of
// blank lines into a single paragraph break.
func cleanText(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(blankRun.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
