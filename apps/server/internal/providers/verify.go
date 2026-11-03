package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var ErrInvalidKey = errors.New("provider rejected the API key")

// VerifyKey checks Groq's authenticated model list without generating a reply.
func VerifyKey(ctx context.Context, baseURL, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("prepare key verification: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("verify provider key: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ErrInvalidKey
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("key verification returned status %d", response.StatusCode)
	}
	return nil
}
