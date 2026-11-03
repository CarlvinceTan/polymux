package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureSender(t *testing.T) *Sender {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "fixture.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), 0600); err != nil {
		t.Fatal(err)
	}
	sender, err := Open(Config{Path: path, KeyID: "TESTKEY123", TeamID: "TESTTEAM12", Topic: "com.flarehq.polymux.app"})
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

func TestAPNsSignsBoundedPrivateAlert(t *testing.T) {
	t.Parallel()
	sender := fixtureSender(t)
	sender.client = &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "api.sandbox.push.apple.com" || request.Header.Get("apns-push-type") != "alert" || len(request.Header.Get("apns-collapse-id")) > 64 {
			t.Fatal("wrong APNs envelope")
		}
		token := strings.TrimPrefix(request.Header.Get("authorization"), "bearer ")
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			t.Fatal("invalid JWT")
		}
		signature, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil || len(signature) != 64 {
			t.Fatal("invalid ES256 signature")
		}
		hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if !ecdsa.Verify(&sender.key.PublicKey, hash[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
			t.Fatal("JWT signature failed")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if len(body) > 4096 || json.Unmarshal(body, &payload) != nil || payload["server"] != "http://server.test:7337" || payload["conversationID"] != "chat" {
			t.Fatal("invalid routing payload")
		}
		if payload["aps"].(map[string]any)["alert"].(map[string]any)["body"] != "Tests pass" {
			t.Fatal("notice body missing")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	item := store.Push{Device: contract.PushDevice{Token: strings.Repeat("ab", 32), Environment: "sandbox", Server: "http://server.test:7337"}, Notice: contract.Notice{Body: "Tests pass", Title: "Agent", ConversationID: "chat", MessageID: "message"}}
	if status := sender.Send(context.Background(), item); status.State != "sent" {
		t.Fatal(status)
	}
	first, err := sender.token()
	if err != nil {
		t.Fatal(err)
	}
	second, err := sender.token()
	if err != nil || first != second {
		t.Fatal("provider tokens unnecessarily regenerated")
	}
	for _, test := range []struct {
		name, body, want string
		status           int
	}{
		{"expired", `{"reason":"Unregistered"}`, "invalid", 410}, {"wrong-topic", `{"reason":"DeviceTokenNotForTopic"}`, "invalid", 400}, {"provider", `{"reason":"InternalServerError"}`, "failed", 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			if status := sender.Send(t.Context(), item); status.State != test.want {
				t.Fatalf("status %s", status.State)
			}
		})
	}
}

func TestAPNsStatusDecidesRetry(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		header  string
		want    string
		retry   bool
		prune   bool
		waitMin int64
		waitMax int64
	}{
		{name: "bad request", status: 400, body: `{"reason":"BadMessageId"}`, header: "30", want: "rejected"},
		{name: "forbidden", status: 403, body: `{"reason":"InvalidProviderToken"}`, want: "rejected"},
		{name: "unregistered", status: 410, body: `{"reason":"Unregistered"}`, want: "invalid", prune: true},
		{name: "throttle", status: 429, body: `{"reason":"TooManyRequests"}`, header: "5", want: "failed", retry: true, waitMin: 3, waitMax: 7},
		{name: "server", status: 500, body: `{"reason":"InternalServerError"}`, header: "5", want: "failed", retry: true, waitMin: 3, waitMax: 7},
		{name: "unavailable", status: 503, body: `{"reason":"ServiceUnavailable"}`, want: "failed", retry: true, waitMin: 90, waitMax: 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, item := claimNotice(t)
			calls := sendStatus(t, db, item, tt.status, tt.body, tt.header, false, tt.want)
			if calls != 1 {
				t.Fatalf("http attempts %d", calls)
			}
			var state string
			var retryAt sql.NullInt64
			var enabled int
			if err := db.DB.QueryRowContext(t.Context(), `SELECT state, retry_at FROM push_queue`).Scan(&state, &retryAt); err != nil {
				t.Fatal(err)
			}
			if err := db.DB.QueryRowContext(t.Context(), `SELECT enabled FROM push_devices`).Scan(&enabled); err != nil {
				t.Fatal(err)
			}
			if tt.prune {
				if state != "invalid" || enabled != 0 {
					t.Fatalf("token not pruned: state %s enabled %d", state, enabled)
				}
				return
			}
			if enabled != 1 {
				t.Fatal("device disabled")
			}
			if !tt.retry {
				if state != tt.want {
					t.Fatalf("state %s", state)
				}
				if _, err := db.DB.ExecContext(t.Context(), `UPDATE push_queue SET retry_at=NULL`); err != nil {
					t.Fatal(err)
				}
				if _, err := db.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
					t.Fatal("status was retried")
				}
				return
			}
			if state != "pending" || !retryAt.Valid {
				t.Fatalf("not queued: %s %v", state, retryAt)
			}
			delta := retryAt.Int64 - time.Now().Unix()
			if delta < tt.waitMin || delta > tt.waitMax {
				t.Fatalf("retry in %ds, want %d-%d", delta, tt.waitMin, tt.waitMax)
			}
			if _, err := db.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("retried before delay")
			}
		})
	}
}

func TestRetryAfterCappedByDeadline(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		deadline time.Duration
		httpDate bool
		min, max int64
	}{
		{name: "header", header: "5", min: 3, max: 7},
		{name: "http date", httpDate: true, min: 6, max: 10},
		{name: "capped", header: "30", deadline: 2 * time.Second, min: -1, max: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, item := claimNotice(t)
			header := tt.header
			if tt.httpDate {
				header = time.Now().Add(8 * time.Second).UTC().Format(http.TimeFormat)
			}
			ctx := t.Context()
			if tt.deadline > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.deadline)
				t.Cleanup(cancel)
			}
			sender := fixtureSender(t)
			sender.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
				h := make(http.Header)
				h.Set("Retry-After", header)
				body := io.NopCloser(strings.NewReader(`{"reason":"TooManyRequests"}`))
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: h, Body: body}, nil
			})
			result := sender.Send(ctx, item)
			if result.State != "failed" || !result.HasAfter {
				t.Fatalf("result %+v", result)
			}
			if tt.deadline > 0 && result.After > tt.deadline {
				t.Fatalf("delay %s exceeds deadline %s", result.After, tt.deadline)
			}
			if err := db.FinishPush(t.Context(), item, result); err != nil {
				t.Fatal(err)
			}
			var retryAt int64
			if err := db.DB.QueryRowContext(t.Context(), `SELECT retry_at FROM push_queue`).Scan(&retryAt); err != nil {
				t.Fatal(err)
			}
			delta := retryAt - time.Now().Unix()
			if delta < tt.min || delta > tt.max {
				t.Fatalf("retry in %ds, want %d-%d", delta, tt.min, tt.max)
			}
		})
	}
}

func TestNetworkErrorStaysUnknown(t *testing.T) {
	db, item := claimNotice(t)
	calls := sendStatus(t, db, item, 0, "", "", true, "unknown")
	if calls != 1 {
		t.Fatalf("http attempts %d", calls)
	}
	var state string
	if err := db.DB.QueryRowContext(t.Context(), `SELECT state FROM push_queue`).Scan(&state); err != nil || state != "unknown" {
		t.Fatalf("state %s %v", state, err)
	}
	if _, err := db.ClaimPush(t.Context()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("network error was replayed")
	}
}

func sendStatus(t *testing.T, db *store.Store, item store.Push, status int, body, header string, netErr bool, want string) int {
	t.Helper()
	var calls int
	sender := fixtureSender(t)
	sender.client.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		if netErr {
			return nil, errors.New("connection reset")
		}
		h := make(http.Header)
		if header != "" {
			h.Set("Retry-After", header)
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	result := sender.Send(t.Context(), item)
	if result.State != want {
		t.Fatalf("state %s", result.State)
	}
	if err := db.FinishPush(t.Context(), item, result); err != nil {
		t.Fatal(err)
	}
	return calls
}

func claimNotice(t *testing.T) (*store.Store, store.Push) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Register(t.Context(), []contract.Agent{{ID: "a", Name: "Same"}}); err != nil {
		t.Fatal(err)
	}
	device := contract.PushDevice{
		ID: "fixture-installation", Token: strings.Repeat("ab", 32),
		Environment: "sandbox", Server: "http://server.test:7337", Enabled: true,
	}
	if err := db.RegisterPush(t.Context(), device); err != nil {
		t.Fatal(err)
	}
	text := "Fixture notice"
	agent := "a"
	message := contract.Message{
		ID: "reply", ThreadID: "a", AgentID: &agent, Role: "assistant", Status: "done",
		CreatedAt: store.Now(), Parts: []contract.Part{{Kind: "text", Text: &text}},
	}
	if _, err := db.Append(t.Context(), contract.Event{ConversationID: "a", Type: "message.done"}, &message); err != nil {
		t.Fatal(err)
	}
	item, err := db.ClaimPush(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return db, item
}
