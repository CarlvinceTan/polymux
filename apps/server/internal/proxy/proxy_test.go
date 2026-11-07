package proxy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/iotest"
)

const secret = "proxy-test-secret-value"

func TestProxyFillsBoundOrigin(t *testing.T) {
	var seen []string
	bound := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		_, password, _ := r.BasicAuth()
		seen = []string{r.URL.Query().Get("key"), r.Header.Get("X-Api-Key"), string(data), password}
		fmt.Fprintf(w, "echo %s %s", r.Header.Get("X-Api-Key"), url.QueryEscape(r.Header.Get("X-Api-Key")))
	}))
	defer bound.Close()
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = []string{r.Header.Get("X-Api-Key")}
	}))
	defer other.Close()
	origin := bound.URL
	fill := func(_ context.Context, owner, target, text string, escape func(string) string) (string, []string) {
		if owner != "bot" || target != origin || !strings.Contains(text, "{{secret:key}}") {
			return text, nil
		}
		value := secret
		if escape != nil {
			value = escape(value)
		}
		return strings.ReplaceAll(text, "{{secret:key}}", value), []string{secret}
	}
	p, err := Start(t.TempDir(), fill, func(_ context.Context, owner, o string) bool { return owner == "bot" && o == origin })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	upstream := x509.NewCertPool()
	upstream.AddCert(bound.Certificate())
	upstream.AddCert(other.Certificate())
	p.transport.TLSClientConfig = &tls.Config{RootCAs: upstream}
	client := func(proxy string) *http.Client {
		address, _ := url.Parse(proxy)
		roots := x509.NewCertPool()
		roots.AddCert(p.authority.cert)
		roots.AddCert(other.Certificate())
		return &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(address), TLSClientConfig: &tls.Config{RootCAs: roots}}}
	}
	request, _ := http.NewRequest("POST", bound.URL+"/echo?key=%7B%7Bsecret%3Akey%7D%7D", strings.NewReader(`{"token":"{{secret:key}}"}`))
	request.Header.Set("X-Api-Key", "{{secret:key}}")
	request.SetBasicAuth("owner", "{{secret:key}}")
	response, err := client(p.Env("bot")["HTTPS_PROXY"]).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if seen[0] != secret || seen[1] != secret || seen[2] != `{"token":"`+secret+`"}` || seen[3] != secret {
		t.Fatalf("references were not filled: %q", seen)
	}
	if strings.Contains(string(data), secret) || string(data) != "echo [redacted] [redacted]" {
		t.Fatalf("reply exposed the value: %q", data)
	}
	// Another origin is tunnelled untouched: the command sees the real certificate and the literal reference.
	request, _ = http.NewRequest("GET", other.URL, nil)
	request.Header.Set("X-Api-Key", "{{secret:key}}")
	if response, err = client(p.Env("bot")["HTTPS_PROXY"]).Do(request); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if seen[0] != "{{secret:key}}" {
		t.Fatalf("a reference was filled for another origin: %q", seen)
	}
	if _, err := client("http://polymux:wrong@" + p.listener.Addr().String()).Get(other.URL); err == nil {
		t.Fatal("an unknown proxy token was accepted")
	}
}

func TestRedactorSplitValue(t *testing.T) {
	source := io.NopCloser(iotest.OneByteReader(strings.NewReader("before " + secret + " after " + secret)))
	data, err := io.ReadAll(newRedactor(source, []string{secret}))
	if err != nil || string(data) != "before [redacted] after [redacted]" {
		t.Fatalf("got %q, %v", data, err)
	}
}

// TestCommandEnvironmentIsTrusted covers what a Go CLI such as gh does: trust the bundle
// the proxy exports, send CONNECT through it, and get references filled.
func TestCommandEnvironmentIsTrusted(t *testing.T) {
	bound := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "echo %s", r.Header.Get("X-Api-Key"))
	}))
	defer bound.Close()
	fill := func(_ context.Context, owner, target, text string, escape func(string) string) (string, []string) {
		if owner != "bot" || target != bound.URL || !strings.Contains(text, "{{secret:key}}") {
			return text, nil
		}
		return strings.ReplaceAll(text, "{{secret:key}}", secret), []string{secret}
	}
	p, err := Start(t.TempDir(), fill, func(_ context.Context, owner, origin string) bool {
		return owner == "bot" && origin == bound.URL
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	env := p.Env("bot")
	bundle, err := os.ReadFile(env["SSL_CERT_FILE"])
	if err != nil {
		t.Fatal("no CA bundle for commands", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundle) {
		t.Fatal("CA bundle has no certificates")
	}
	upstream := x509.NewCertPool()
	upstream.AddCert(bound.Certificate())
	p.transport.TLSClientConfig = &tls.Config{RootCAs: upstream}
	address, _ := url.Parse(env["HTTPS_PROXY"])
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(address), TLSClientConfig: &tls.Config{RootCAs: roots}}}
	request, _ := http.NewRequest("GET", bound.URL+"/user", nil)
	request.Header.Set("X-Api-Key", "{{secret:key}}")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if string(data) != "echo [redacted]" {
		t.Fatalf("filled request or redacted reply missing: %q", data)
	}
}
