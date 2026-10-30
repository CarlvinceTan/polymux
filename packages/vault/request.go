package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

type Request struct {
	ID     string            `json:"id"`
	URL    string            `json:"url"`
	Method string            `json:"method"`
	Auth   string            `json:"auth"`
	Fields map[string]string `json:"fields"`
	Values map[string]string `json:"values"`
	Form   bool              `json:"form"`
}

type Receipt struct {
	Status int `json:"status"`
}

func client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Transport:     transport,
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Request sends values directly to the saved origin and returns no response body, headers, or cookies.
func (s *Store) Request(ctx context.Context, input Request) (Receipt, error) {
	origin, err := Origin(input.URL)
	if err != nil {
		return Receipt{}, ErrInvalid
	}
	if input.Method == "" {
		input.Method = "POST"
	}
	if !slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"}, input.Method) {
		return Receipt{}, ErrInvalid
	}
	if len(input.Fields) > 32 || len(input.Values) > 32 {
		return Receipt{}, ErrInvalid
	}
	fields := []string{}
	for name, field := range input.Fields {
		if len(name) == 0 || len(name) > 128 || strings.ContainsAny(name, "\r\n\x00") {
			return Receipt{}, ErrInvalid
		}
		if _, exists := input.Values[name]; exists {
			return Receipt{}, ErrInvalid
		}
		fields = append(fields, field)
	}
	for name, value := range input.Values {
		if len(name) == 0 || len(name) > 128 || len(value) > 4096 {
			return Receipt{}, ErrInvalid
		}
	}
	switch input.Auth {
	case "":
	case "bearer":
		fields = append(fields, "token")
	case "basic":
		fields = append(fields, "username", "password")
	default:
		return Receipt{}, ErrInvalid
	}
	if len(fields) == 0 {
		return Receipt{}, ErrInvalid
	}
	if (input.Method == "GET" || input.Method == "HEAD") && (len(input.Fields)+len(input.Values) > 0) {
		return Receipt{}, ErrInvalid
	}
	// Validate the complete request before consuming a recovery code.
	if _, err := http.NewRequestWithContext(ctx, input.Method, input.URL, nil); err != nil {
		return Receipt{}, ErrInvalid
	}
	values, err := s.Values(ctx, input.ID, origin, fields)
	if err != nil {
		return Receipt{}, err
	}
	defer clear(values)
	body := map[string]string{}
	for name, value := range input.Values {
		body[name] = value
	}
	for name, field := range input.Fields {
		body[name] = values[field]
	}
	data, _ := json.Marshal(body)
	contentType := "application/json"
	if input.Form {
		form := url.Values{}
		for name, value := range body {
			form.Set(name, value)
		}
		data = []byte(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}
	defer clear(data)
	if len(body) == 0 {
		data = nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, input.Method, input.URL, bytes.NewReader(data))
	if err != nil {
		return Receipt{}, ErrInvalid
	}
	req.Header.Set("Content-Type", contentType)
	switch input.Auth {
	case "bearer":
		if strings.ContainsAny(values["token"], "\r\n\x00") {
			return Receipt{}, ErrInvalid
		}
		req.Header.Set("Authorization", "Bearer "+values["token"])
	case "basic":
		req.SetBasicAuth(values["username"], values["password"])
	}
	transport := client()
	defer transport.CloseIdleConnections()
	response, err := transport.Do(req)
	if err != nil {
		return Receipt{}, errors.New("vault request failed; check the destination before retrying")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
	return Receipt{Status: response.StatusCode}, nil
}
