package textutil

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var skipTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Nav: true,
	atom.Header: true, atom.Footer: true, atom.Aside: true, atom.Form: true,
	atom.Iframe: true, atom.Svg: true, atom.Button: true, atom.Select: true,
	atom.Figure: true, atom.Figcaption: true,
}

var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true,
	atom.Li: true, atom.Tr: true, atom.Blockquote: true,
}

// StripTags returns the visible text of an HTML fragment on one line. It is
// used for feed titles and summaries, which often carry markup and entities.
func StripTags(fragment string) string {
	if !strings.ContainsAny(fragment, "<&") {
		return strings.Join(strings.Fields(fragment), " ")
	}
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return strings.Join(strings.Fields(fragment), " ")
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && skipTags[n.DataAtom] {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return strings.Join(strings.Fields(b.String()), " ")
}

// ArticleText extracts the readable body of a news page: the text of the
// paragraph-like blocks inside the container that holds the most paragraph
// text, skipping navigation, scripts and other page furniture. Paragraphs are
// separated by newlines.
func ArticleText(page string) string {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return ""
	}
	best, bestLen := doc, 0
	var score func(*html.Node) int
	score = func(n *html.Node) int {
		if n.Type == html.ElementNode && skipTags[n.DataAtom] {
			return 0
		}
		total := 0
		direct := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s := score(c)
			total += s
			if c.Type == html.ElementNode && c.DataAtom == atom.P {
				direct += s
			}
		}
		if n.Type == html.TextNode {
			return len(strings.TrimSpace(n.Data))
		}
		// The article container is the node whose own <p> children carry the
		// most text; ancestors only add menus and related-links blocks.
		if direct > bestLen {
			best, bestLen = n, direct
		}
		return total
	}
	score(doc)

	var paras []string
	var cur strings.Builder
	flush := func() {
		t := strings.Join(strings.Fields(cur.String()), " ")
		cur.Reset()
		if t != "" {
			paras = append(paras, t)
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && skipTags[n.DataAtom] {
			return
		}
		block := n.Type == html.ElementNode && blockTags[n.DataAtom]
		if block {
			flush()
		}
		if n.Type == html.TextNode {
			cur.WriteString(n.Data)
			cur.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if block || (n.Type == html.ElementNode && n.DataAtom == atom.Br) {
			flush()
		}
	}
	walk(best)
	flush()
	return strings.Join(paras, "\n")
}

// PageTitle returns the <title> of an HTML page.
func PageTitle(page string) string {
	z := html.NewTokenizer(strings.NewReader(page))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			if t := z.Token(); t.DataAtom == atom.Title {
				if z.Next() == html.TextToken {
					return strings.Join(strings.Fields(z.Token().Data), " ")
				}
				return ""
			}
		}
	}
}
