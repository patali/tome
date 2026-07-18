// Package epubgen turns an extracted Article into a Kindle-friendly EPUB.
//
// EPUB (reflowable) is the pragmatic delivery format: it renders well on every
// Kindle and Amazon's Send-to-Kindle accepts it. A device-accurate Typst PDF is
// a later phase and needs the `typst` binary, which the EPUB path avoids.
package epubgen

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	epub "github.com/go-shiori/go-epub"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/patali/tome/server/internal/article"
)

// eink CSS — reflowable, high-contrast, mirrors the reader.html spec.
const einkCSS = `
body { font-family: Georgia, "Times New Roman", serif; line-height: 1.55;
  color: #000; background: #fff; }
h1 { font-size: 1.6em; line-height: 1.2; margin: 0 0 0.3em; }
h2 { font-size: 1.3em; margin: 1.4em 0 0.4em; }
h3 { font-size: 1.1em; margin: 1.2em 0 0.3em; }
p  { margin: 0 0 0.7em; text-align: justify; }
a  { color: #000; text-decoration: underline; }
.tome-meta { font-style: italic; color: #555; font-size: 0.85em;
  margin: 0 0 1.4em; border-bottom: 1px solid #ccc; padding-bottom: 0.8em; }
blockquote { margin: 1em 0; padding-left: 1em; border-left: 2px solid #ccc;
  font-style: italic; }
pre { font-family: "Courier New", monospace; font-size: 0.85em; background: #f0f0f0;
  padding: 0.7em; white-space: pre-wrap; word-wrap: break-word; }
code { font-family: "Courier New", monospace; background: #f0f0f0; }
pre code { background: transparent; }
img { max-width: 100%; height: auto; }
table { border-collapse: collapse; width: 100%; font-size: 0.9em; }
th, td { border: 1px solid #ccc; padding: 4px 6px; text-align: left; }
th { background: #f0f0f0; }
hr { border: none; border-top: 1px solid #ccc; }
`

// voidRe self-closes HTML void elements so the body is XHTML-well-formed for EPUB.
var voidRe = regexp.MustCompile(`(?i)<(img|br|hr|source|meta|input|area|base|col|embed|link|param|track|wbr)(\b[^>]*?)\s*/?>`)

// toXHTML normalizes Readability's HTML5 into EPUB-safe XHTML: it balances the
// tree via a real parser, then ensures void elements are self-closed.
func toXHTML(fragment string) string {
	// The context node MUST carry DataAtom (not just Data) or ParseFragment
	// errors and we lose entity decoding — leaving raw &nbsp;/&mdash; that make
	// the XHTML invalid. Parsing decodes every named entity to a real character.
	ctx := &html.Node{Type: html.ElementNode, DataAtom: atom.Body, Data: "body"}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), ctx)
	if err == nil {
		var buf bytes.Buffer
		ok := true
		for _, n := range nodes {
			if renderErr := html.Render(&buf, n); renderErr != nil {
				ok = false
				break
			}
		}
		if ok {
			fragment = buf.String()
		}
	}
	return voidRe.ReplaceAllString(fragment, `<$1$2 />`)
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func metaLine(a article.Article) string {
	var parts []string
	if a.Byline != "" {
		parts = append(parts, esc(a.Byline))
	}
	if a.PublishedTime != "" {
		if t, err := time.Parse(time.RFC3339, a.PublishedTime); err == nil {
			parts = append(parts, "Published "+t.Format("Jan 2, 2006"))
		}
	}
	if a.URL != "" {
		parts = append(parts, "Source: "+esc(a.URL))
	}
	if len(parts) == 0 {
		return ""
	}
	return `<p class="tome-meta">` + strings.Join(parts, " &#183; ") + `</p>`
}

// Build renders the article to EPUB bytes and returns them with a filename.
func Build(a article.Article) (data []byte, filename string, err error) {
	title := strings.TrimSpace(a.Title)
	if title == "" {
		title = "Untitled X Article"
	}

	book, err := epub.NewEpub(title)
	if err != nil {
		return nil, "", fmt.Errorf("new epub: %w", err)
	}
	if a.Byline != "" {
		book.SetAuthor(a.Byline)
	}
	if a.URL != "" {
		book.SetDescription("Source: " + a.URL)
	}

	cssFile, err := os.CreateTemp("", "tome-*.css")
	if err != nil {
		return nil, "", fmt.Errorf("temp css: %w", err)
	}
	defer os.Remove(cssFile.Name())
	if _, err = cssFile.WriteString(einkCSS); err != nil {
		return nil, "", err
	}
	cssFile.Close()

	cssPath, err := book.AddCSS(cssFile.Name(), "eink.css")
	if err != nil {
		return nil, "", fmt.Errorf("add css: %w", err)
	}

	body := "<h1>" + esc(title) + "</h1>" + metaLine(a) + toXHTML(a.Content)
	if _, err = book.AddSection(body, title, "article.xhtml", cssPath); err != nil {
		return nil, "", fmt.Errorf("add section: %w", err)
	}

	epubFile, err := os.CreateTemp("", "tome-*.epub")
	if err != nil {
		return nil, "", fmt.Errorf("temp epub: %w", err)
	}
	epubFile.Close()
	defer os.Remove(epubFile.Name())

	if err = book.Write(epubFile.Name()); err != nil {
		return nil, "", fmt.Errorf("write epub: %w", err)
	}
	data, err = os.ReadFile(epubFile.Name())
	if err != nil {
		return nil, "", err
	}
	return data, a.FileName(".epub"), nil
}
