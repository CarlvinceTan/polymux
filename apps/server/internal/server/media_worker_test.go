package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
)

func TestMediaWorker(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprintf("success=%t", success), func(t *testing.T) {
			api, _ := startTest(t, &voiceFactory{})
			var checks atomic.Int32
			var endpoint string
			web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/download" {
					w.Write(mediaMP4)
					return
				}
				checks.Add(1)
				if success {
					fmt.Fprintf(w, `{"done":true,"error":null,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":%q}}]}}}`, endpoint+"/download")
				} else {
					fmt.Fprint(w, `{"done":true,"error":{"message":"blocked"}}`)
				}
			}))
			defer web.Close()
			endpoint = web.URL
			mediaProvider(t, api, "gemini", endpoint+"/v1beta")
			now := time.Now().Add(time.Hour)
			job := mediaJob{ID: strings.Repeat("1", 32), Owner: "assistant", Conversation: "assistant", Kind: "video", Prompt: "A kite", Provider: "media-test", Type: "gemini", BaseURL: endpoint + "/v1beta", Model: "test-model", Status: "pending", Operation: "models/test-model/operations/one", Message: "original-reply", Call: "original-call", NextAt: now.Unix()}
			path := filepath.Join(api.config.DataDir, "media-jobs", job.ID+".json")
			if err := saveMediaJob(path, job); err != nil {
				t.Fatal(err)
			}
			// Changed configuration pauses the existing job without submitting elsewhere.
			if err := api.settings.SetProviders(nil); err != nil {
				t.Fatal(err)
			}
			api.checkMedia(t.Context(), now)
			if checks.Load() != 0 {
				t.Fatal("disabled provider was contacted")
			}
			mediaProvider(t, api, "gemini", endpoint+"/v1beta")
			api.checkMedia(t.Context(), now.Add(time.Minute))
			message, err := api.Store.Message(t.Context(), "media-result:"+job.ID)
			if err != nil || len(message.Parts) != 1 {
				t.Fatal("background result unavailable", message, err)
			}
			if success && (message.Parts[0].Kind != "attachment" || message.Parts[0].Attachment == nil || message.Parts[0].Attachment.Kind != "video") {
				t.Fatal("video did not use native attachment part", message)
			}
			if !success && (message.Parts[0].Kind != "text" || message.Parts[0].Text == nil || !strings.Contains(*message.Parts[0].Text, "without a video")) {
				t.Fatal("failure was not surfaced", message)
			}
			data, err := os.ReadFile(path)
			if err != nil || json.Unmarshal(data, &job) != nil || !job.Delivered {
				t.Fatal("completion not persisted", err)
			}
			job.Delivered = false
			if err := saveMediaJob(path, job); err != nil {
				t.Fatal(err)
			}
			// Model a crash after the atomic publication but before the delivered marker.
			api.checkMedia(t.Context(), now.Add(2*time.Minute))
			var count int
			events, err := api.Store.Events(t.Context(), job.Conversation, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Type == "message.end" && event.MessageID != nil && *event.MessageID == message.ID {
					count++
				}
			}
			if count != 1 {
				t.Fatal("background result was published twice", count, err)
			}
			if checks.Load() != 1 {
				t.Fatal("completed job was polled or submitted again", checks.Load())
			}
		})
	}
}

func TestMediaSelection(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	providers := []settings.Provider{
		{ID: "unsupported", Type: "xai", Enabled: true, Order: 0, Models: []settings.Model{{ID: "unsupported-image", Enabled: true, Tags: []string{"image-gen"}}}},
		{ID: "supported", Type: "openai", Enabled: true, Order: 1, Models: []settings.Model{{ID: "supported-image", Enabled: true, Tags: []string{"image-gen"}}}},
	}
	if err := api.settings.SetProviders(providers); err != nil {
		t.Fatal(err)
	}
	tool := &mediaTool{server: api, files: api.outputs, kind: "image"}
	choices, err := tool.candidates()
	if err != nil || len(choices) != 1 || choices[0].provider.ID != "supported" || choices[0].model != "supported-image" {
		t.Fatal("auto selection did not use supported tagged model", choices, err)
	}
	order := func(models ...contract.MediaStep) {
		t.Helper()
		custom := contract.MediaOrder{Mode: contract.MediaOrderModeCustom, Models: models}
		if _, err := api.settings.Update(contract.SettingsPatch{ImageOrder: &custom}); err != nil {
			t.Fatal(err)
		}
	}
	order(contract.MediaStep{Provider: "missing", Model: "missing"})
	if _, err := tool.candidates(); err == nil {
		t.Fatal("custom order silently fell back to another model")
	}
	order(contract.MediaStep{Provider: "unsupported", Model: "unsupported-image"}, contract.MediaStep{Provider: "supported", Model: "supported-image"})
	choices, err = tool.candidates()
	if err != nil || len(choices) != 1 || choices[0].provider.ID != "supported" {
		t.Fatal("custom order did not skip the unsupported step", choices, err)
	}
}
