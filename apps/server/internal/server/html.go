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
const htmlPrompt = `Display one self-contained interactive HTML card inside the message bubble. Polymux owns the outer bubble, its rounded corners, and clipping; write edge-to-edge inner content only.

Canvas
- No outer card wrapper, body margins, outer radius, or outer drop shadow. html/body background, padding, and gradients are fine.
- Inline CSS and JS only; images are data: URLs. External URLs, links, form submissions, popups, imports, and CDNs are blocked.
- Put one inline <style> and one inline <script> last. LiquidGlass loads after your markup, so run setup that reads component properties inside a DOMContentLoaded listener.
- To let the user answer you, call polymuxSend('Week') from a click or change handler: Polymux sends that text to you as the user's next message, so make it self-contained, such as 'Focus: week'.
- Keep ids unique; write text with textContent; never replace a component's innerHTML.

Components (preloaded, theme-aware; not native SwiftUI views)
- <lg-button>Save</lg-button> action; tone="clear|tinted|light", disabled; native click. To change a label, update a nested <span id> or set the button's textContent.
- <lg-switch checked>Notify</lg-switch> boolean; .checked; native change.
- <lg-slider value="40" min="0" max="100" step="1" label="Volume"></lg-slider> number; .value; native input and change.
- <lg-segmented value="week" label="Range"><button value="day">Day</button><button value="week">Week</button></lg-segmented> picker with two to four direct <button value> options; .value; change with detail.value.
- <lg-panel> insets a frosted group; never the card's outer wrapper; on an image, pass style="--lg-surface:transparent" so controls stay glassy.
- Page-level only, do not use: lg-navbar, lg-menubar, lg-menu, lg-dock, lg-lens, toasts, createGlass. Other fields use plain input, select, or textarea.

Layout
- Controls in .row (12px gap, wraps) or .stack (12px vertical gap); .muted dims secondary text; headings and lists are styled.
- Glass copies the surface behind it: the card canvas already provides the window colour, so controls read as glass without setup. If you paint your own background or art, mark that element with data-lg-backdrop (data-lg-backdrop="img" refracts an image inside) and keep the controls inside it.
- Theme tokens (--lg-accent, --lg-surface, --lg-tint, --lg-track) can be overridden inline; prefer the defaults.

Size
- width 44-550 and height 44-400 CSS px. Omit width to size the card to its content up to 550; declare a width only when the layout needs a fixed viewport. Omit height to measure up to 400, then the card scrolls internally. Phones can narrow a requested width, so avoid fixed vh/vw and keep layouts responsive.

Example: <style>#card{display:grid;gap:12px;padding:16px}</style><div id="card"><h3>Weekly focus</h3><p class="muted"><span id="out">4</span> h planned</p><lg-slider label="Hours" value="4" min="0" max="12"></lg-slider><div class="row"><lg-button>Save</lg-button></div></div><script>const s=document.querySelector('lg-slider'),o=document.getElementById('out');s.addEventListener('input',()=>{o.textContent=s.value;});document.querySelector('lg-button').addEventListener('click',()=>polymuxSend('Hours: '+s.value));</script>

The tool returns a receipt only; never repeat the HTML as message text.`

type htmlKey struct{}

type htmlScope struct {
	messageID string
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
				"height": map[string]any{"type": "number", "minimum": 44, "maximum": 400, "description": "Optional fixed native viewport height in logical CSS pixels; overflow scrolls vertically. Omit for measured content height capped at 400."},
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
	a = contract.Attachment{ID: id, Kind: "html", Name: name, MimeType: "text/html", Size: int64(len(args.HTML)), URL: "/attachments/" + id + "/content", IndexingState: "ready", ExtractedText: &text, Width: width, Height: height}
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

// cardControls are the Liquid Glass components a card may use.
var cardControls = map[string]bool{
	"lg-button": true, "lg-switch": true, "lg-slider": true, "lg-segmented": true, "lg-panel": true,
}

// cardPage components only work on full pages, not in a card frame.
var cardPage = map[string]bool{
	"lg-navbar": true, "lg-menubar": true, "lg-menu": true, "lg-dock": true, "lg-lens": true, "lg-toast": true,
}

// cardBanned elements cannot load or navigate inside a card.
var cardBanned = map[string]bool{
	"link": true, "base": true, "iframe": true, "frame": true, "frameset": true,
	"object": true, "embed": true, "applet": true, "portal": true,
}

const cardList = "<lg-button>, <lg-switch>, <lg-slider>, <lg-segmented>, or <lg-panel>"

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
			switch {
			case cardControls[tag]:
			case cardPage[tag]:
				report("<" + tag + "> is page-level; use " + cardList)
			case strings.HasPrefix(tag, "lg-"):
				report("unknown <" + tag + ">; use " + cardList)
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
			case "lg-segmented":
				options := 0
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type != html.ElementNode {
						continue
					}
					if c.Data != "button" {
						report("<lg-segmented> options must be direct <button> children")
						continue
					}
					options++
				}
				if options < 2 {
					report("<lg-segmented> needs two or more <button value> options")
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
