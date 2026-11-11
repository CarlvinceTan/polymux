package hub

import (
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	signatureTags = []atom.Atom{atom.Div, atom.P, atom.Br, atom.Span, atom.B, atom.Strong, atom.I, atom.Em, atom.U, atom.S, atom.Strike,
		atom.Font, atom.A, atom.Ul, atom.Ol, atom.Li}
	signatureAttributes = []string{"style", "face", "size", "color", "align", "href"}
	signatureStyles     = []string{"color", "font-family", "font-size", "font-style", "font-weight", "text-align", "text-decoration", "text-decoration-line"}
	unsafeStyle         = regexp.MustCompile(`(?i)url\s*\(|expression\s*\(|@import`)
	safeLink            = regexp.MustCompile(`(?i)^(https?:|mailto:)`)
)

func cleanStyle(value string) string {
	kept := []string{}
	for _, rule := range strings.Split(value, ";") {
		name, setting, ok := strings.Cut(rule, ":")
		name, setting = strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(setting)
		if ok && slices.Contains(signatureStyles, name) && !unsafeStyle.MatchString(setting) {
			kept = append(kept, name+": "+setting)
		}
	}
	return strings.Join(kept, "; ")
}

func cleanNode(node *html.Node) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		switch child.Type {
		case html.ElementNode:
			cleanNode(child)
			if !slices.Contains(signatureTags, child.DataAtom) {
				// Unknown markup goes, but the words inside a harmless wrapper stay.
				if child.DataAtom != atom.Script && child.DataAtom != atom.Style {
					for grandchild := child.FirstChild; grandchild != nil; {
						after := grandchild.NextSibling
						child.RemoveChild(grandchild)
						node.InsertBefore(grandchild, child)
						grandchild = after
					}
				}
				node.RemoveChild(child)
				break
			}
			kept := child.Attr[:0]
			for _, attribute := range child.Attr {
				key := strings.ToLower(attribute.Key)
				switch {
				case !slices.Contains(signatureAttributes, key):
					continue
				case key == "style":
					attribute.Val = cleanStyle(attribute.Val)
					if attribute.Val == "" {
						continue
					}
				case key == "href":
					if child.DataAtom != atom.A || !safeLink.MatchString(strings.TrimSpace(attribute.Val)) {
						continue
					}
				}
				kept = append(kept, attribute)
			}
			child.Attr = kept
		case html.CommentNode, html.DoctypeNode:
			node.RemoveChild(child)
		}
		child = next
	}
}

// cleanSignature keeps only the small email-safe subset the signature editor can make.
func cleanSignature(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(value), context)
	if err != nil {
		return ""
	}
	root := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, node := range nodes {
		root.AppendChild(node)
	}
	cleanNode(root)
	var out strings.Builder
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if html.Render(&out, child) != nil {
			return ""
		}
	}
	return out.String()
}

var (
	scriptBlock = regexp.MustCompile(`(?is)<script[\s\S]*?</script>|<style[\s\S]*?</style>`)
	lineBreak   = regexp.MustCompile(`(?i)<br\s*/?>`)
	blockEnd    = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6])>`)
	anyTag      = regexp.MustCompile(`<[^>]+>`)
	blankRun    = regexp.MustCompile(`\n{3,}`)
)

// htmlText flattens markup, for a preview and for a message that has no plain text.
func htmlText(value string) string {
	value = scriptBlock.ReplaceAllString(value, "")
	value = lineBreak.ReplaceAllString(value, "\n")
	value = blockEnd.ReplaceAllString(value, "\n")
	value = anyTag.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, " ", " ")
	return strings.TrimSpace(blankRun.ReplaceAllString(value, "\n\n"))
}
