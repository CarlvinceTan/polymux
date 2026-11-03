package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

type Config struct {
	Path, KeyID, TeamID, Topic string
	// Endpoint replaces the Apple host. Empty in production; tests point it at httptest.
	Endpoint string
}
type Sender struct {
	config Config
	key    *ecdsa.PrivateKey
	client *http.Client
	mu     sync.Mutex
	jwt    string
	issued time.Time
}

func Open(config Config) (*Sender, error) {
	if config.Path == "" || len(config.KeyID) != 10 || len(config.TeamID) != 10 || config.Topic == "" || strings.ContainsAny(config.Topic, "\r\n") {
		return nil, errors.New("incomplete APNs configuration")
	}
	info, err := os.Stat(config.Path)
	if err != nil {
		return nil, errors.New("APNs key file unavailable")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return nil, errors.New("APNs key requires an owner-only regular file")
	}
	data, err := os.ReadFile(config.Path)
	if err != nil {
		return nil, errors.New("APNs key file unavailable")
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid APNs signing key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid APNs signing key")
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("APNs requires an ES256 key")
	}
	return &Sender{config: config, key: key, client: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (s *Sender) token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jwt != "" && time.Since(s.issued) < 40*time.Minute {
		return s.jwt, nil
	}
	now := time.Now()
	header, _ := json.Marshal(map[string]any{"alg": "ES256", "kid": s.config.KeyID})
	claims, _ := json.Marshal(map[string]any{"iss": s.config.TeamID, "iat": now.Unix()})
	encode := base64.RawURLEncoding.EncodeToString
	payload := encode(header) + "." + encode(claims)
	hash := sha256.Sum256([]byte(payload))
	r, v, err := ecdsa.Sign(rand.Reader, s.key, hash[:])
	if err != nil {
		return "", err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	v.FillBytes(signature[32:])
	s.jwt = payload + "." + encode(signature)
	s.issued = now
	return s.jwt, nil
}

func (s *Sender) Send(ctx context.Context, push store.Push) store.PushResult {
	token, err := s.token()
	if err != nil {
		return store.PushResult{State: "failed"}
	}
	data, _ := json.Marshal(map[string]any{"aps": map[string]any{"alert": map[string]string{"title": push.Notice.Title, "body": push.Notice.Body}, "sound": "default", "thread-id": push.Notice.ConversationID}, "server": push.Device.Server, "conversationID": push.Notice.ConversationID, "messageID": push.Notice.MessageID})
	if len(data) > 4096 {
		return store.PushResult{State: "failed"}
	}
	endpoint := "https://api.push.apple.com"
	if push.Device.Environment == "sandbox" {
		endpoint = "https://api.sandbox.push.apple.com"
	}
	if s.config.Endpoint != "" {
		endpoint = strings.TrimRight(s.config.Endpoint, "/")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/3/device/"+push.Device.Token, bytes.NewReader(data))
	if err != nil {
		return store.PushResult{State: "failed"}
	}
	request.Header.Set("authorization", "bearer "+token)
	request.Header.Set("apns-topic", s.config.Topic)
	request.Header.Set("apns-push-type", "alert")
	request.Header.Set("apns-priority", "10")
	request.Header.Set("apns-expiration", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	hash := sha256.Sum256([]byte(strconv.FormatInt(push.Notice.Seq, 10)))
	request.Header.Set("apns-collapse-id", base64.RawURLEncoding.EncodeToString(hash[:]))
	response, err := s.client.Do(request)
	if err != nil {
		return store.PushResult{State: "unknown"}
	}
	defer response.Body.Close()
	return classify(ctx, response)
}

// classify retries only 429 and 5xx. 400 and 403 are terminal. A dropped connection stays unknown.
func classify(ctx context.Context, response *http.Response) store.PushResult {
	if response.StatusCode == http.StatusOK {
		return store.PushResult{State: "sent"}
	}
	var result struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result)
	if response.StatusCode == http.StatusGone || result.Reason == "BadDeviceToken" || result.Reason == "DeviceTokenNotForTopic" {
		return store.PushResult{State: "invalid"}
	}
	retryable := response.StatusCode == http.StatusTooManyRequests
	retryable = retryable || (response.StatusCode >= 500 && response.StatusCode < 600)
	if retryable {
		after, ok := parseRetryAfter(ctx, response.Header.Get("Retry-After"))
		return store.PushResult{State: "failed", After: after, HasAfter: ok}
	}
	return store.PushResult{State: "rejected"}
}

func parseRetryAfter(ctx context.Context, header string) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	var delay time.Duration
	if seconds, err := strconv.Atoi(header); err == nil {
		if seconds < 0 {
			seconds = 0
		}
		delay = time.Duration(seconds) * time.Second
	} else if when, err := http.ParseTime(header); err == nil {
		delay = time.Until(when)
	} else {
		return 0, false
	}
	if delay < 0 {
		delay = 0
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline); remain < delay {
			if remain < 0 {
				return 0, true
			}
			return remain, true
		}
	}
	return delay, true
}
