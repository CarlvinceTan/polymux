package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"golang.org/x/net/html"
)

const watchLimit = 1 << 20

func (s *Server) runWatches(ctx context.Context) {
	defer s.workers.Done()
	client := watchClient()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		s.checkWatches(ctx, client, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type publicOnly struct{}

// watchClient follows redirects only where the watched URL itself could reach: a public
// URL never reaches a loopback, private or link-local address, even through DNS.
func watchClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if ctx.Value(publicOnly{}) == nil {
			return dialer.DialContext(ctx, network, addr)
		}
		guarded := *dialer
		guarded.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if ip := net.ParseIP(host); err != nil || ip == nil || !publicIP(ip) {
				return errors.New("watch redirect reached a local address")
			}
			return nil
		}
		return guarded.DialContext(ctx, network, addr)
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || r.URL.User != nil || r.URL.Scheme != "http" && r.URL.Scheme != "https" {
			return errors.New("unsupported watch redirect")
		}
		// A watch the user pointed at a local service may only redirect within that host.
		if r.Context().Value(publicOnly{}) == nil && r.URL.Hostname() != via[0].URL.Hostname() {
			return errors.New("unsupported watch redirect")
		}
		return nil
	}}
}

// carrierNAT is 100.64.0.0/10, which Tailscale uses for tailnet peers.
var carrierNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func publicIP(ip net.IP) bool {
	return !carrierNAT.Contains(ip) && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

// localWatch reports whether a watched URL names a local service, by address or DNS.
func localWatch(ctx context.Context, raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return !publicIP(ip)
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		if !publicIP(addr.IP) {
			return true
		}
	}
	return false
}

func (s *Server) checkWatches(ctx context.Context, client *http.Client, now time.Time) {
	if !s.admission.start() {
		return
	}
	defer s.admission.done()
	items, err := s.Store.DueWatches(ctx, now)
	if err != nil {
		if ctx.Err() == nil {
			s.config.Log.Error("read content watches", "error", err)
		}
		return
	}
	var workers sync.WaitGroup
	queue := make(chan contract.Routine)
	for range min(4, len(items)) {
		workers.Go(func() {
			for routine := range queue {
				content, err := readWatch(ctx, client, routine.Watch)
				if err := s.Store.CheckWatch(ctx, routine, content, err, now); err != nil && ctx.Err() == nil {
					s.config.Log.Error("save content watch", "error", err)
				}
			}
		})
	}
	for _, routine := range items {
		select {
		case queue <- routine:
		case <-ctx.Done():
			close(queue)
			workers.Wait()
			return
		}
	}
	close(queue)
	workers.Wait()
}

func readWatch(ctx context.Context, client *http.Client, watch *contract.RoutineWatch) (string, error) {
	if !localWatch(ctx, watch.URL) {
		ctx = context.WithValue(ctx, publicOnly{}, true)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, watch.URL, nil)
	if err != nil {
		return "", errors.New("invalid watch URL")
	}
	request.Header.Set("Accept", "application/json, text/html, text/plain")
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New("watch request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("watch returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, watchLimit+1))
	if err != nil {
		return "", errors.New("watch response could not be read")
	}
	if len(body) > watchLimit || !utf8.Valid(body) {
		return "", errors.New("watch response must be UTF-8 and at most 1 MiB")
	}
	kind := strings.ToLower(response.Header.Get("Content-Type"))
	if len(watch.JsonPointers) > 0 || strings.Contains(kind, "json") || json.Valid(body) {
		return watchJSON(body, watch.JsonPointers)
	}
	if strings.Contains(kind, "html") {
		root, err := html.Parse(bytes.NewReader(body))
		if err != nil {
			return "", errors.New("watch HTML could not be read")
		}
		var text strings.Builder
		var visit func(*html.Node)
		visit = func(node *html.Node) {
			if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style" || node.Data == "noscript" || node.Data == "template") {
				return
			}
			if node.Type == html.TextNode {
				text.WriteString(node.Data)
				text.WriteByte(' ')
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(root)
		return strings.Join(strings.Fields(text.String()), " "), nil
	}
	if kind != "" && !strings.HasPrefix(kind, "text/") {
		return "", errors.New("watch response must be JSON, HTML, or text")
	}
	return strings.Join(strings.Fields(string(body)), " "), nil
}

func watchJSON(body []byte, pointers []string) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || !json.Valid(body) {
		return "", errors.New("watch response is not valid JSON")
	}
	if len(pointers) > 0 {
		selected := make(map[string]any, len(pointers))
		for _, pointer := range pointers {
			item, found := jsonPointer(value, pointer)
			selected[pointer] = map[string]any{"present": found, "value": item}
		}
		value = selected
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(data) > watchLimit {
		return "", errors.New("selected watch content exceeds 1 MiB")
	}
	return string(data), nil
}

func jsonPointer(value any, pointer string) (any, bool) {
	if pointer == "" {
		return value, true
	}
	for _, token := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch object := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = object[token]
			if !ok {
				return nil, false
			}
		case []any:
			if token == "" || strings.IndexFunc(token, func(char rune) bool { return char < '0' || char > '9' }) >= 0 {
				return nil, false
			}
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(object) || len(token) > 1 && token[0] == '0' {
				return nil, false
			}
			value = object[index]
		default:
			return nil, false
		}
	}
	return value, true
}
