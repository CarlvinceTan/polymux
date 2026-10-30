// Package proxy is a loopback HTTP proxy for agent commands. A command writes a secret
// reference such as {{secret:name}} where a value belongs; the proxy fills it in on the way
// to the origin the secret is bound to and redacts it from the reply, so the command, and the
// agent reading its output, never holds the value.
package proxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Fill replaces the references in text that are usable at target, escaping values for where
// they land, and returns the values it used.
type Fill func(ctx context.Context, owner, target, text string, escape func(string) string) (string, []string)

// Bound reports whether owner has a secret bound to origin, so its traffic is worth opening.
type Bound func(ctx context.Context, owner, origin string) bool

// bodyLimit bounds a request body the proxy rewrites; larger bodies pass through unchanged.
const bodyLimit = 8 << 20

// encodedRef is a reference that a URL or form encoder has percent-encoded.
var encodedRef = regexp.MustCompile(`(?i)%7B%7B(secret|vault)(?:%3A|:)([A-Za-z0-9_-]{1,128})(?:(?:%3A|:)([A-Za-z]{1,64}))?%7D%7D`)

type Proxy struct {
	fill      Fill
	bound     Bound
	authority *authority
	listener  net.Listener
	server    *http.Server
	transport *http.Transport
	caFile    string
	bundle    string
	mu        sync.Mutex
	tokens    map[string]string
	owners    map[string]string
}

// Start listens on loopback and writes the CA files commands trust into dir.
func Start(dir string, fill Fill, bound Bound) (*Proxy, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	authority, err := newAuthority()
	if err != nil {
		return nil, err
	}
	p := &Proxy{fill: fill, bound: bound, authority: authority, caFile: filepath.Join(dir, "ca.pem"),
		tokens: map[string]string{}, owners: map[string]string{}}
	if err := os.WriteFile(p.caFile, authority.pem, 0600); err != nil {
		return nil, err
	}
	// Tools that replace their trust store with this file must still trust public sites.
	if roots := systemRoots(); roots != nil {
		p.bundle = filepath.Join(dir, "bundle.pem")
		if err := os.WriteFile(p.bundle, append(append(roots, '\n'), authority.pem...), 0600); err != nil {
			return nil, err
		}
	}
	p.transport = &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, MaxIdleConns: 32, IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 5 * time.Minute}
	if p.listener, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		return nil, err
	}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = p.server.Serve(p.listener) }()
	return p, nil
}

func (p *Proxy) Close() error {
	p.transport.CloseIdleConnections()
	return p.server.Close()
}

// Env routes a command's web traffic through the proxy as owner.
func (p *Proxy) Env(owner string) map[string]string {
	address := "http://polymux:" + p.token(owner) + "@" + p.listener.Addr().String()
	env := map[string]string{
		"HTTPS_PROXY": address, "https_proxy": address, "HTTP_PROXY": address, "http_proxy": address,
		"NODE_EXTRA_CA_CERTS": p.caFile, "NODE_USE_ENV_PROXY": "1",
	}
	if p.bundle != "" {
		for _, name := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO"} {
			env[name] = p.bundle
		}
	}
	return env
}

func (p *Proxy) token(owner string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if token, ok := p.owners[owner]; ok {
		return token
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	token := hex.EncodeToString(raw)
	p.owners[owner], p.tokens[token] = token, owner
	return token
}

func (p *Proxy) owner(r *http.Request) (string, bool) {
	scheme, value, _ := strings.Cut(r.Header.Get("Proxy-Authorization"), " ")
	decoded, err := base64.StdEncoding.DecodeString(value)
	if !strings.EqualFold(scheme, "Basic") || err != nil {
		return "", false
	}
	_, token, _ := strings.Cut(string(decoded), ":")
	p.mu.Lock()
	defer p.mu.Unlock()
	owner, ok := p.tokens[token]
	return owner, ok
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner, ok := p.owner(r)
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="polymux"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		p.connect(w, r, owner)
		return
	}
	if !r.URL.IsAbs() || r.URL.Scheme != "http" {
		http.Error(w, "use an absolute http URL or CONNECT", http.StatusBadRequest)
		return
	}
	response, err := p.forward(r, owner, "http", r.URL.Host)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for name, values := range response.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(flushWriter{w}, response.Body)
}

// connect opens traffic to an origin with a bound secret and tunnels everything else untouched.
func (p *Proxy) connect(w http.ResponseWriter, r *http.Request, owner string) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "tunnel unavailable", http.StatusInternalServerError)
		return
	}
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		http.Error(w, "invalid CONNECT target", http.StatusBadRequest)
		return
	}
	origin := "https://" + strings.ToLower(host)
	if port != "443" {
		origin += ":" + port
	}
	intercept := p.bound(r.Context(), owner, origin)
	var upstream net.Conn
	if !intercept {
		if upstream, err = net.DialTimeout("tcp", r.Host, 15*time.Second); err != nil {
			http.Error(w, "upstream unreachable", http.StatusBadGateway)
			return
		}
	}
	raw, buffered, err := hijacker.Hijack()
	if err != nil {
		if upstream != nil {
			upstream.Close()
		}
		return
	}
	client := bufferedConn{Conn: raw, reader: buffered.Reader}
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		client.Close()
		if upstream != nil {
			upstream.Close()
		}
		return
	}
	if !intercept {
		go func() {
			_, _ = io.Copy(upstream, client)
			upstream.Close()
		}()
		_, _ = io.Copy(client, upstream)
		client.Close()
		return
	}
	p.intercept(client, owner, strings.TrimPrefix(origin, "https://"))
}

func (p *Proxy) intercept(conn net.Conn, owner, host string) {
	defer conn.Close()
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}
	leaf, err := p.authority.leaf(name)
	if err != nil {
		return
	}
	secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{*leaf}, NextProtos: []string{"http/1.1"}, MinVersion: tls.VersionTLS12})
	reader := bufio.NewReader(secure)
	for {
		request, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		response, err := p.forward(request, owner, "https", host)
		if err != nil {
			_, _ = io.WriteString(secure, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
			return
		}
		// The client speaks HTTP/1.1 here whatever the upstream used.
		response.Proto, response.ProtoMajor, response.ProtoMinor = "HTTP/1.1", 1, 1
		if response.ContentLength < 0 {
			response.TransferEncoding = []string{"chunked"}
		}
		err = response.Write(secure)
		response.Body.Close()
		if err != nil || request.Close || response.Close {
			return
		}
	}
}

// hopHeaders apply to one connection and are never forwarded.
var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authorization", "Proxy-Authenticate", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

// forward fills references in the request, sends it on, and redacts the filled values from the reply.
func (p *Proxy) forward(r *http.Request, owner, scheme, host string) (*http.Response, error) {
	ctx := r.Context()
	base := scheme + "://" + host
	uri := r.RequestURI
	if r.URL.IsAbs() {
		uri = r.URL.RequestURI()
	}
	var used []string
	fill := func(text string, escape func(string) string) string {
		text = encodedRef.ReplaceAllStringFunc(text, func(match string) string {
			m := encodedRef.FindStringSubmatch(match)
			ref := "{{" + m[1] + ":" + m[2]
			if m[3] != "" {
				ref += ":" + m[3]
			}
			filled, values := p.fill(ctx, owner, base, ref+"}}", url.QueryEscape)
			if len(values) == 0 {
				return match
			}
			used = append(used, values...)
			return filled
		})
		filled, values := p.fill(ctx, owner, base, text, escape)
		used = append(used, values...)
		return filled
	}
	path, query, hasQuery := strings.Cut(uri, "?")
	uri = fill(path, url.PathEscape)
	if hasQuery {
		uri += "?" + fill(query, url.QueryEscape)
	}
	var body io.Reader = r.Body
	length := r.ContentLength
	if r.Body != nil && r.ContentLength <= bodyLimit {
		data, err := io.ReadAll(io.LimitReader(r.Body, bodyLimit+1))
		if err != nil {
			return nil, err
		}
		if len(data) <= bodyLimit {
			filled := fill(string(data), nil)
			body, length = strings.NewReader(filled), int64(len(filled))
		} else {
			body = io.MultiReader(strings.NewReader(string(data)), r.Body)
		}
	}
	request, err := http.NewRequestWithContext(ctx, r.Method, base+uri, body)
	if err != nil {
		return nil, err
	}
	request.ContentLength = length
	if length == 0 {
		request.Body = http.NoBody
	}
	for name, values := range r.Header {
		request.Header[name] = append([]string(nil), values...)
	}
	for _, name := range hopHeaders {
		request.Header.Del(name)
	}
	for name, values := range request.Header {
		for i, value := range values {
			if scheme, token, ok := strings.Cut(value, " "); ok && strings.EqualFold(scheme, "Basic") && name == "Authorization" {
				// Basic auth hides a reference inside base64.
				if decoded, err := base64.StdEncoding.DecodeString(token); err == nil {
					if filled := fill(string(decoded), nil); filled != string(decoded) {
						encoded := base64.StdEncoding.EncodeToString([]byte(filled))
						used = append(used, encoded)
						values[i] = scheme + " " + encoded
						continue
					}
				}
			}
			values[i] = fill(value, nil)
		}
	}
	if len(used) > 0 {
		// An encoded reply can't be redacted, so ask for a plain one.
		request.Header.Del("Accept-Encoding")
	}
	response, err := p.transport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	for _, name := range hopHeaders {
		response.Header.Del(name)
	}
	if len(used) > 0 {
		response.Body = newRedactor(response.Body, used)
		response.ContentLength = -1
		response.Header.Del("Content-Length")
	}
	return response, nil
}

type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(data []byte) (int, error) {
	n, err := f.w.Write(data)
	if flusher, ok := f.w.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}

// systemRoots reads the host's public CA bundle so a command trusting only our bundle still
// reaches public sites. Without it, Go CLIs such as gh would reject the proxy's own certificate.
func systemRoots() []byte {
	var roots []byte
	for _, path := range []string{os.Getenv("SSL_CERT_FILE"), "/etc/ssl/cert.pem", "/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/ca-bundle.pem"} {
		if path == "" {
			continue
		}
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
	}
	if runtime.GOOS == "darwin" {
		// macOS keeps no root file; export the system trust store instead.
		for _, keychain := range []string{"/System/Library/Keychains/SystemRootCertificates.keychain", "/Library/Keychains/System.keychain"} {
			out, err := exec.Command("/usr/bin/security", "find-certificate", "-a", "-p", keychain).Output()
			if err == nil && len(out) > 0 {
				roots = append(roots, out...)
			}
		}
	}
	return roots
}

// bufferedConn reads through the bytes the HTTP server had already buffered.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c bufferedConn) Read(data []byte) (int, error) { return c.reader.Read(data) }
