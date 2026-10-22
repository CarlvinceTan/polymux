package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

const mediaPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="

var mediaMP4 = []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}

func mediaContext(t *testing.T, api *Server, message string) (context.Context, *[]contract.Attachment) {
	t.Helper()
	ctx, attachments := outputContext(t, api, message, t.TempDir())
	return context.WithValue(ctx, turnKey{}, &turnState{run: store.NewRun("user", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}}), attachments
}

func mediaProvider(t *testing.T, api *Server, kind, endpoint string) {
	t.Helper()
	if err := api.settings.SetProviders([]settings.Provider{{ID: "media-test", Name: "Media", Type: kind, BaseURL: endpoint, Enabled: true,
		Headers: map[string]string{"Authorization": "Bearer media-test-key"}, Models: []settings.Model{{ID: "test-model", Enabled: true, Tags: []string{"image-gen", "video-gen"}}}}}); err != nil {
		t.Fatal(err)
	}
}

func TestMediaImage(t *testing.T) {
	for _, kind := range []string{"openai", "gemini"} {
		t.Run(kind, func(t *testing.T) {
			api, _ := startTest(t, &voiceFactory{})
			var requests atomic.Int32
			web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer media-test-key" {
					t.Error("incorrect image request or credential")
				}
				jobs, _ := filepath.Glob(filepath.Join(api.config.DataDir, "media-jobs", "*.json"))
				if len(jobs) != 1 {
					t.Error("submission has no durable intent")
				} else if data, _ := os.ReadFile(jobs[0]); !strings.Contains(string(data), `"status":"uncertain"`) {
					t.Error("intent was not recorded before paid submission")
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("invalid image body")
				}
				if kind == "openai" {
					if r.URL.Path != "/v1/images/generations" || body["model"] != "test-model" || body["prompt"] != "A blue kite" || body["response_format"] != nil {
						t.Error("incorrect OpenAI image protocol", r.URL.Path, body)
					}
					fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, mediaPNG)
				} else {
					if r.URL.Path != "/v1/models/test-model:generateContent" || body["contents"] == nil || body["generationConfig"] == nil {
						t.Error("incorrect Gemini image protocol", r.URL.Path, body)
					}
					fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"thought":true,"inlineData":{"mimeType":"image/png","data":"aW52YWxpZA=="}},{"inlineData":{"mimeType":"image/png","data":%q}}]}}]}`, mediaPNG)
				}
			}))
			defer web.Close()
			mediaProvider(t, api, kind, web.URL+"/v1")
			ctx, attachments := mediaContext(t, api, "image-reply")
			tool := &mediaTool{server: api, files: api.outputs, kind: "image"}
			for range 2 {
				result, err := tool.Execute(ctx, "image-call", json.RawMessage(`{"prompt":"A blue kite"}`), nil)
				if err != nil || !strings.Contains(result.Text(), "Generated image.png") {
					t.Fatal("image failed", result.Text(), err)
				}
			}
			if requests.Load() != 1 || len(*attachments) != 1 || (*attachments)[0].Kind != "image" {
				t.Fatal("image was not replayed from the stored output", requests.Load(), *attachments)
			}
			if _, err := tool.Execute(ctx, "image-call", json.RawMessage(`{"prompt":"Different"}`), nil); err == nil {
				t.Fatal("reused call identity accepted a changed prompt")
			}
		})
	}
}

func TestMediaUncertain(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	var requests atomic.Int32
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "credential=media-test-key", 500)
	}))
	defer web.Close()
	mediaProvider(t, api, "openai", web.URL+"/v1")
	ctx, _ := mediaContext(t, api, "uncertain-reply")
	for range 2 {
		// A fresh tool instance models a server/runtime restart on the same journal.
		tool := &mediaTool{server: api, files: api.outputs, kind: "image"}
		_, err := tool.Execute(ctx, "uncertain-call", json.RawMessage(`{"prompt":"A kite"}`), nil)
		if err == nil || strings.Contains(err.Error(), "media-test-key") {
			t.Fatal("failure missing or credential leaked", err)
		}
	}
	if requests.Load() != 1 {
		t.Fatal("uncertain paid request resubmitted", requests.Load())
	}
}

func TestMediaVideo(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	var submissions, checks atomic.Int32
	var endpoint string
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer media-test-key" {
			t.Error("video credential missing")
		}
		switch r.URL.Path {
		case "/v1beta/models/test-model:predictLongRunning":
			submissions.Add(1)
			if r.Method != http.MethodPost {
				t.Error("video submission method")
			}
			fmt.Fprint(w, `{"name":"models/test-model/operations/video-one"}`)
		case "/v1beta/models/test-model/operations/video-one":
			if checks.Add(1) == 1 {
				fmt.Fprint(w, `{"done":false}`)
			} else {
				fmt.Fprintf(w, `{"done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":%q}}]}}}`, endpoint+"/download?alt=media")
			}
		case "/download":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write(mediaMP4)
		default:
			t.Error("unknown video request", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer web.Close()
	endpoint = web.URL
	mediaProvider(t, api, "gemini", web.URL+"/v1beta")
	ctx, attachments := mediaContext(t, api, "video-reply")
	tool := &mediaTool{server: api, files: api.outputs, kind: "video"}
	result, err := tool.Execute(ctx, "video-submit", json.RawMessage(`{"prompt":"A kite moves"}`), nil)
	if err != nil || !strings.Contains(result.Text(), "pending") {
		t.Fatal(result.Text(), err)
	}
	var job struct {
		ID string `json:"job_id"`
	}
	if err := json.Unmarshal(result.StructuredContent, &job); err != nil || job.ID == "" {
		t.Fatal("missing job ID", err)
	}
	args, _ := json.Marshal(map[string]string{"job_id": job.ID})
	// A new instance polls the persisted job without a second paid submission.
	tool = &mediaTool{server: api, files: api.outputs, kind: "video"}
	for i := range 2 {
		result, err = tool.Execute(ctx, fmt.Sprintf("poll-%d", i), args, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if submissions.Load() != 1 || checks.Load() != 2 || len(*attachments) != 1 || (*attachments)[0].Kind != "video" {
		t.Fatal("incorrect video lifecycle", submissions.Load(), checks.Load(), *attachments)
	}
	if _, err := tool.Execute(ctx, "poll-1", args, nil); err != nil || checks.Load() != 2 {
		t.Fatal("completed job was requested again", err)
	}
	other := context.WithValue(ctx, turnKey{}, &turnState{run: store.NewRun("other", "assistant", "assistant", "message"), work: store.Work{Trigger: "message"}})
	otherScope, _ := other.Value(htmlKey{}).(*htmlScope)
	copyScope := *otherScope
	copyScope.messageID = "other-reply"
	other = context.WithValue(other, htmlKey{}, &copyScope)
	path := filepath.Join(api.config.DataDir, "media-jobs", job.ID+".json")
	data, _ := os.ReadFile(path)
	var record mediaJob
	json.Unmarshal(data, &record)
	record.Conversation = "a-private-group"
	if err := saveMediaJob(path, record); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(other, "foreign-poll", args, nil); err == nil {
		t.Fatal("another conversation could read media job")
	}
}

func TestMediaProviderBoundaries(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	mediaProvider(t, api, "xai", "https://api.example.com/v1")
	if err := api.settings.SetCapabilities(map[string]settings.Capability{"image-gen": {Choice: json.RawMessage(`{"provider":"media-test","model":"test-model"}`)}}); err != nil {
		t.Fatal(err)
	}
	tool := &mediaTool{server: api, files: api.outputs, kind: "image"}
	if _, _, err := tool.selection(); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatal("unsupported adapter was advertised", err)
	}
	provider := settings.Provider{ID: "vault-media", Type: "gemini", BaseURL: "https://example.com/v1"}
	tool.key = func(context.Context, string, settings.Provider) (string, error) { return "vault-test-key", nil }
	headers, err := tool.headers(t.Context(), "assistant", provider)
	if err != nil || headers.Get("x-goog-api-key") != "vault-test-key" || headers.Get("Authorization") != "" {
		t.Fatal("Gemini credential protocol", err)
	}
	if _, err := tool.download(t.Context(), provider.BaseURL, "https://other.example.com/video", headers); err == nil {
		t.Fatal("credential could be sent to arbitrary download origin")
	}
	provider.Headers = map[string]string{"Authorization": "Bearer invalid-configured-key"}
	tool.key = func(context.Context, string, settings.Provider) (string, error) {
		t.Fatal("configured authentication fell back silently")
		return "", nil
	}
	if _, err := tool.headers(t.Context(), "assistant", provider); err != nil {
		t.Fatal(err)
	}
	image, _ := base64.StdEncoding.DecodeString(mediaPNG)
	if !strings.HasPrefix(http.DetectContentType(image), "image/") || http.DetectContentType(mediaMP4) != "video/mp4" {
		t.Fatal("invalid fixture media")
	}
}

func TestMediaDeliveryCancellation(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	var submissions atomic.Int32
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		submissions.Add(1)
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, mediaPNG)
	}))
	defer web.Close()
	mediaProvider(t, api, "openai", web.URL+"/v1")
	base, _ := mediaContext(t, api, "cancelled-delivery")
	ctx, cancel := context.WithCancel(base)
	scope := *ctx.Value(htmlKey{}).(*htmlScope)
	scope.publish = func(context.Context, contract.Attachment, string) error { cancel(); return context.Canceled }
	ctx = context.WithValue(ctx, htmlKey{}, &scope)
	tool := &mediaTool{server: api, files: api.outputs, kind: "image"}
	if _, err := tool.Execute(ctx, "cancelled-call", json.RawMessage(`{"prompt":"A blue kite"}`), nil); err == nil {
		t.Fatal("cancelled delivery succeeded")
	}
	ctx, attachments := mediaContext(t, api, "cancelled-delivery")
	// Delivery uses the cached bytes, even if credentials or provider selection changed.
	if err := api.settings.SetProviders(nil); err != nil {
		t.Fatal(err)
	}
	tool = &mediaTool{server: api, files: api.outputs, kind: "image"}
	if _, err := tool.Execute(ctx, "cancelled-call", json.RawMessage(`{"prompt":"A blue kite"}`), nil); err != nil || len(*attachments) != 1 || submissions.Load() != 1 {
		t.Fatal("cancelled delivery lost the generated result or resubmitted it", err, submissions.Load())
	}
}
