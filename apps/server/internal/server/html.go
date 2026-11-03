package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"golang.org/x/net/html"
)

const htmlLimit = 1 << 20

// htmlPrompt is the model-facing contract for card markup.
const htmlPrompt = `Display one self-contained HTML card inside the message bubble: a visualisation, a small tool, a simulation, a game, or an interactive answer. Cards are sandboxed mini-apps: canvas, WebGL, SVG, animation, Web Audio, Web Workers and WebAssembly all work; network and browser storage don't. The bubble supplies the card's background, radius and edge: write edge-to-edge inner content only (no outer wrapper, border, background, margins, radius or shadow, and don't wrap everything in a panel); pad your own content, 16px is typical. Inline CSS and JS only; images are data: URLs; external URLs, links, forms, popups and CDNs are blocked. A styled kit and Chart.js 4 are preloaded: read the card reference for its classes, colour tokens and chart rules before building anything beyond plain text.

polymuxSend(textOrJSON) from a click or change handler sends the user's reply to this card (one per gesture, under 16 KB; sends without a real gesture are ignored). polymuxSave(value) keeps up to 64 KB of JSON state; restore from polymuxState when the card is shown again.

Size: width 44-550 CSS px; omit to measure the content. The card grows to show all of its content and never scrolls as a whole, so keep it compact: aim for under 400 CSS px tall, and use tabs, paging, collapsible sections or a second card for more. Only small parts may scroll inside it, such as a wide table or code block; never the main content. height 44-400 sets a minimum, for a canvas or game that needs a set size. Size content in px, not vh. Keep layouts responsive; phones may narrow the width.

The tool returns a receipt only; never repeat the HTML as message text.`

type htmlKey struct{}

type htmlScope struct {
	messageID string
	cwd       string
	publish   func(context.Context, contract.Attachment, string) error
}

type htmlTool struct {
	store *store.Store
	dir   string
	mu    sync.Mutex
}

func (*htmlTool) Name() string                           { return "show_html" }
func (*htmlTool) Label() string                          { return "Show visual card" }
func (*htmlTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*htmlTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{
		Name:        "show_html",
		Description: htmlPrompt,
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"html":   map[string]any{"type": "string", "minLength": 1, "maxLength": htmlLimit, "description": "Inner HTML body content or fragment with inline resources; no outer card wrapper, body margins, outer rounded corners, or drop shadow."},
				"title":  map[string]any{"type": "string", "maxLength": 128, "description": "Short accessible card title."},
				"width":  map[string]any{"type": "number", "minimum": 44, "maximum": 550, "description": "Optional fixed native viewport width in logical CSS pixels. Omit to size the card to its rendered content, up to 550. Responsive content must fit narrower phone widths."},
				"height": map[string]any{"type": "number", "minimum": 44, "maximum": 400, "description": "Optional minimum height in logical CSS pixels, for a canvas or game that needs a set size; taller content still grows the card. Omit to measure the content."},
			},
			"required": []string{"html"},
		},
	}
}

func (t *htmlTool) Execute(ctx context.Context, callID string, params json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var empty agent.AgentToolResult
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	scope, ok := ctx.Value(htmlKey{}).(*htmlScope)
	if !ok || scope.publish == nil || scope.messageID == "" {
		return empty, errors.New("visual cards require an active reply")
	}
	if callID == "" {
		return empty, errors.New("visual card tool call has no identity")
	}
	var args struct {
		HTML   string          `json:"html"`
		Title  string          `json:"title"`
		Width  json.RawMessage `json:"width"`
		Height json.RawMessage `json:"height"`
	}
	decoder := json.NewDecoder(bytes.NewReader(params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return empty, fmt.Errorf("read visual card: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return empty, errors.New("visual card arguments must contain one object")
	}
	if strings.TrimSpace(args.HTML) == "" || len(args.HTML) > htmlLimit || !utf8.ValidString(args.HTML) {
		return empty, errors.New("visual card HTML must be nonempty UTF-8 up to 1 MB")
	}
	if utf8.RuneCountInString(args.Title) > 128 || !utf8.ValidString(args.Title) {
		return empty, errors.New("visual card title must be UTF-8 up to 128 characters")
	}
	if issues := cardIssues(args.HTML); len(issues) > 0 {
		return empty, fmt.Errorf("fix the visual card HTML: %s", strings.Join(issues, "; "))
	}
	width, err := htmlDimension(args.Width, 550)
	if err != nil {
		return empty, fmt.Errorf("visual card width: %w", err)
	}
	height, err := htmlDimension(args.Height, 400)
	if err != nil {
		return empty, fmt.Errorf("visual card height: %w", err)
	}
	title := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, args.Title)
	name := filepath.Base(strings.ReplaceAll(strings.TrimSpace(title), "\\", "/"))
	if name == "." || name == ".." || name == "/" || name == "" {
		name = "Visual card"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".html") {
		name += ".html"
	}
	sum := sha256.Sum256([]byte(scope.messageID + "\x00" + callID))
	id := hex.EncodeToString(sum[:16])
	path := filepath.Join(t.dir, id)

	// One tool call identity always refers to one immutable artifact.
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	a, storedPath, err := t.store.Attachment(ctx, id)
	if err == nil {
		data, err := os.ReadFile(storedPath)
		if err != nil {
			return empty, fmt.Errorf("read visual card: %w", err)
		}
		if string(data) != args.HTML || a.Name != name || !sameDimension(a.Width, width) || !sameDimension(a.Height, height) {
			return empty, errors.New("visual card tool identity was reused with different content")
		}
		if err := scope.publish(ctx, a, storedPath); err != nil {
			return empty, err
		}
		return htmlReceipt(a), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, fmt.Errorf("find visual card: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return empty, fmt.Errorf("create visual card: %w", err)
	}
	_, writeErr := io.WriteString(f, args.HTML)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(path)
		return empty, fmt.Errorf("write visual card: %w", err)
	}
	text := htmlText(args.HTML)
	a = contract.Attachment{ID: id, Kind: contract.AttachmentKindHTML, Name: name, MimeType: "text/html", Size: int64(len(args.HTML)), URL: "/attachments/" + id + "/content", IndexingState: contract.AttachmentIndexingStateReady, ExtractedText: &text, Width: width, Height: height}
	if err := ctx.Err(); err != nil {
		os.Remove(path)
		return empty, err
	}
	if err := scope.publish(ctx, a, path); err != nil {
		os.Remove(path)
		return empty, fmt.Errorf("show visual card: %w", err)
	}
	return htmlReceipt(a), nil
}

func htmlDimension(raw json.RawMessage, limit float64) (*float64, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value float64
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil || value < 44 || value > limit {
		return nil, fmt.Errorf("must be a number from 44 to %.0f CSS pixels", limit)
	}
	return &value, nil
}

func sameDimension(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// cardBanned elements cannot load or navigate inside a card.
var cardBanned = map[string]bool{
	"link": true, "base": true, "iframe": true, "frame": true, "frameset": true,
	"object": true, "embed": true, "applet": true, "portal": true,
}

// cardIssues reports card markup that would fail silently at render time.
func cardIssues(source string) []string {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return []string{"HTML could not be parsed"}
	}
	var issues []string
	seen := map[string]bool{}
	report := func(issue string) {
		if len(issues) < 4 && !seen[issue] {
			seen[issue] = true
			issues = append(issues, issue)
		}
	}
	ids := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := n.Data
			if strings.HasPrefix(tag, "lg-") {
				report("<" + tag + "> does not exist; use plain HTML controls from the kit")
			}
			if cardBanned[tag] {
				report("<" + tag + "> is blocked in cards")
			}
			for _, a := range n.Attr {
				switch a.Key {
				case "id":
					if a.Val != "" {
						if ids[a.Val] {
							report("duplicate id \"" + a.Val + "\"")
						}
						ids[a.Val] = true
					}
				case "src":
					switch {
					case a.Val == "":
					case tag == "script":
						report("<script src> is blocked in cards; use an inline script")
					case !strings.HasPrefix(a.Val, "data:"):
						report("<" + tag + " src> must be a data: URL")
					}
				case "srcset":
					if a.Val != "" {
						report("<" + tag + " srcset> is not allowed; use one src data: URL")
					}
				case "poster":
					if a.Val != "" && !strings.HasPrefix(a.Val, "data:") {
						report("<" + tag + " poster> must be a data: URL")
					}
				case "href":
					if a.Val != "" && !strings.HasPrefix(a.Val, "#") {
						report("<" + tag + " href> cannot navigate in a card; use a button and inline script")
					}
				case "action":
					if a.Val != "" {
						report("<form action> cannot submit in a card; use a button and inline script")
					}
				case "style":
					if cssExternal(a.Val) {
						report("style has an external url(); inline data: URLs only")
					}
				case "http-equiv":
					if strings.EqualFold(a.Val, "refresh") {
						report("<meta http-equiv=refresh> is blocked in cards")
					}
				}
			}
			switch tag {
			case "script":
				return // script bodies may contain anything
			case "style":
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.TextNode {
						continue
					}
					if strings.Contains(strings.ToLower(c.Data), "@import") {
						report("@import is blocked in cards")
					}
					if cssExternal(c.Data) {
						report("style has an external url(); inline data: URLs only")
					}
				}
			}
			if hasClass(n, "segmented") {
				options := 0
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.ElementNode {
						continue
					}
					if c.Data != "button" {
						report(".segmented options must be direct <button> children")
						continue
					}
					options++
				}
				if options < 2 {
					report(".segmented needs two or more <button value> options")
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return issues
}

func hasClass(n *html.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" && slices.Contains(strings.Fields(a.Val), name) {
			return true
		}
	}
	return false
}

// cssExternal reports an external resource in CSS, which the card sandbox blocks.
func cssExternal(css string) bool {
	source := strings.ToLower(css)
	for {
		index := strings.Index(source, "url(")
		if index < 0 {
			return false
		}
		source = strings.TrimLeft(source[index+4:], " \t\r\n")
		if strings.HasPrefix(source, `"`) || strings.HasPrefix(source, "'") {
			source = source[1:]
		}
		if !strings.HasPrefix(source, "data:") && !strings.HasPrefix(source, "#") {
			return true
		}
	}
}

func htmlReceipt(a contract.Attachment) agent.AgentToolResult {
	data, _ := json.Marshal(map[string]string{"attachmentID": a.ID, "title": strings.TrimSuffix(a.Name, ".html")})
	return agent.AgentToolResult{
		Content:           []ai.ToolResultMessageContent{ai.TextContent{Text: fmt.Sprintf("Displayed %q in the conversation.", strings.TrimSuffix(a.Name, ".html"))}},
		StructuredContent: data,
	}
}

func htmlText(source string) string {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if text.Len() >= 64<<10 {
			return
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "head", "script", "style", "template", "noscript":
				return
			}
		}
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
			text.WriteByte(' ')
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	out := strings.Join(strings.Fields(text.String()), " ")
	if len(out) > 64<<10 {
		out = out[:64<<10]
		for !utf8.ValidString(out) {
			out = out[:len(out)-1]
		}
	}
	return out
}
