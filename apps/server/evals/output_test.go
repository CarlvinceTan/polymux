package evals

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
)

func TestOUT1GeneratedFile(t *testing.T) {
	const report = "The amber release is ready."
	h := Start(t, Options{ID: "OUT1", Agents: []agentruntime.Definition{{Agent: contract.Agent{ID: agentID, Name: "Assistant", Model: evalModel}, Tools: []string{"write"}}}, Steps: []Step{
		Fixed(nil, ToolReply("out1-write", "write", map[string]any{"path": "report.txt", "content": report})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "out1-write", "") {
				return fmt.Errorf("workspace write did not finish")
			}
			return nil
		}, ToolReply("out1-attach", "attach_file", map[string]any{"path": "report.txt", "name": "Release report.txt"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "out1-attach", "Attached Release report.txt") {
				return fmt.Errorf("generated file did not attach through durable tool context")
			}
			return nil
		}, TextReply("Your release report is attached.")),
		Fixed(nil, ToolReply("out1-private", "attach_file", map[string]any{"path": "report.txt", "name": "Private delivery.txt"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "out1-private", "Attached Private delivery.txt") {
				return fmt.Errorf("private voice output publication failed")
			}
			return nil
		}, TextReply("I sent the report.")),
	}})
	if code := h.Send(agentID, "out1-user", "Write and attach the release report."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("send status %d", code))
	}
	if run := h.WaitRun(agentID, "out1-user", agentID, deadline); run.Status != "completed" {
		h.die("file run status " + run.Status)
	}
	attachments := outputAttachments(h.Messages(agentID))
	if len(attachments) != 1 || attachments[0].Name != "Release report.txt" {
		h.die(fmt.Sprintf("generated attachment missing: %+v", attachments))
	}
	assertOutput(t, h, attachments[0], []byte(report))
	source, err := os.ReadFile(filepath.Join(h.Dir, agentID, "report.txt"))
	if err != nil || string(source) != report {
		h.die("generated workspace source was not preserved")
	}
	h.Restart()
	assertOutput(t, h, attachments[0], []byte(report))
	voice := privateVoice(t, h, "out1-private-request", "Please send the report from our private conversation.")
	if !strings.Contains(voice.Text, "sent the report") {
		h.die("private voice delivery did not finish")
	}
	messages := h.Messages(agentID)
	attachments = outputAttachments(messages)
	if len(attachments) != 2 || attachments[1].Name != "Private delivery.txt" {
		h.die(fmt.Sprintf("private generated file did not reach the typed conversation: %+v", attachments))
	}
	assertOutput(t, h, attachments[1], []byte(report))
	for _, message := range messages {
		if strings.Contains(messageText(message), "Please send the report from our private conversation") || strings.Contains(messageText(message), "I sent the report.") {
			h.die("private consultation transcript was published with the artifact")
		}
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "OUT1", "runtime-created file delivered, downloaded after restart, and privately sent without publishing the voice transcript")
}

func TestMEDIA1NativeGeneration(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII="
	mp4 := []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}
	var submissions, images atomic.Int32
	var providerURL string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer media-eval-fixture" {
			http.Error(w, "credential missing", 401)
			return
		}
		switch r.URL.Path {
		case "/v1beta/models/eval-image:generateContent":
			images.Add(1)
			fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":%q}}]}}]}`, png)
		case "/v1beta/models/eval-video:predictLongRunning":
			submissions.Add(1)
			fmt.Fprint(w, `{"name":"models/eval-video/operations/eval-one"}`)
		case "/v1beta/models/eval-video/operations/eval-one":
			fmt.Fprintf(w, `{"done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":%q}}]}}}`, providerURL+"/video")
		case "/video":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write(mp4)
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	providerURL = provider.URL
	var jobID atomic.Value
	h := Start(t, Options{ID: "MEDIA1", Steps: []Step{
		Fixed(nil, ToolReply("media1-image", "generate_image", map[string]any{"prompt": "A blue kite"})),
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "media1-image", "Attached Generated image.png") {
				return fmt.Errorf("native image generation did not attach")
			}
			if strings.Contains(string(req.Tools), "media-eval-fixture") {
				return fmt.Errorf("credential entered tool schemas")
			}
			return nil
		}, TextReply("The image is attached.")),
		Fixed(nil, ToolReply("media1-video", "generate_video", map[string]any{"prompt": "The kite flies"})),
		Fixed(func(req *ChatRequest) error {
			for _, message := range req.Messages {
				if message.ToolCallID != "media1-video" {
					continue
				}
				match := regexp.MustCompile(`job_id ([0-9a-f]{32})`).FindStringSubmatch(string(message.Content))
				if len(match) == 2 {
					jobID.Store(match[1])
					return nil
				}
			}
			return fmt.Errorf("video submission did not return a persistent job ID")
		}, TextReply("The video is processing.")),
		{Response: func(*ChatRequest) Response {
			id, _ := jobID.Load().(string)
			return ToolReply("media1-poll", "generate_video", map[string]any{"job_id": id})
		}},
		Fixed(func(req *ChatRequest) error {
			if !hasToolResult(req, "media1-poll", "Attached Generated video.mp4") {
				return fmt.Errorf("persisted video job did not finish through the real tool")
			}
			return nil
		}, TextReply("The video is attached.")),
	}})
	preferences, err := settings.Open(filepath.Join(h.Dir, "settings.json"))
	if err != nil {
		h.die(err.Error())
	}
	if err := preferences.SetProviders([]settings.Provider{{ID: "eval-media", Type: "gemini", Name: "Fixture media", BaseURL: provider.URL + "/v1beta", Enabled: true,
		Headers: map[string]string{"Authorization": "Bearer media-eval-fixture"}, Models: []settings.Model{
			{ID: "eval-image", Enabled: true, Tags: []string{"image-gen"}}, {ID: "eval-video", Enabled: true, Tags: []string{"video-gen"}},
		}}}); err != nil {
		h.die(err.Error())
	}
	h.Restart()
	for _, turn := range []struct{ id, text string }{{"media1-user-image", "Generate the kite image."}, {"media1-user-video", "Animate the kite."}} {
		if code := h.Send(agentID, turn.id, turn.text); code != http.StatusAccepted {
			h.die(fmt.Sprintf("media send status %d", code))
		}
		if run := h.WaitRun(agentID, turn.id, agentID, deadline); run.Status != "completed" {
			h.die("media run status " + run.Status)
		}
	}
	h.Restart()
	completed := false
	until := time.Now().Add(25 * time.Second)
	for time.Now().Before(until) {
		if len(outputAttachments(h.Messages(agentID))) == 2 {
			completed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !completed {
		h.die("pending video did not attach automatically after restart without a user turn")
	}
	if code := h.Send(agentID, "media1-user-poll", "Check that video job."); code != http.StatusAccepted {
		h.die(fmt.Sprintf("poll send status %d", code))
	}
	if run := h.WaitRun(agentID, "media1-user-poll", agentID, deadline); run.Status != "completed" {
		h.die("poll run status " + run.Status)
	}
	attachments := outputAttachments(h.Messages(agentID))
	if len(attachments) != 2 || attachments[0].Kind != "image" || attachments[1].Kind != "video" || images.Load() != 1 || submissions.Load() != 1 {
		h.die(fmt.Sprintf("incorrect media results: images=%d submissions=%d attachments=%+v", images.Load(), submissions.Load(), attachments))
	}
	assertOutput(t, h, attachments[1], mp4)
	for _, req := range h.Emulator.Requests() {
		for _, message := range req.Messages {
			if strings.Contains(string(message.Content), "media-eval-fixture") {
				h.die("provider credential leaked into model context")
			}
		}
	}
	if err := h.Emulator.Done(); err != nil {
		h.die(err.Error())
	}
	pass(t, "MEDIA1", "native image attached; video completed automatically after restart with one submission and no duplicate on explicit status recall")
}

func outputAttachments(messages []contract.Message) []contract.Attachment {
	var attachments []contract.Attachment
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Kind == "attachment" && part.Attachment != nil {
				attachments = append(attachments, *part.Attachment)
			}
		}
	}
	return attachments
}

func assertOutput(t *testing.T, h *Harness, attachment contract.Attachment, want []byte) {
	t.Helper()
	response, err := http.Get(h.HTTP.URL + attachment.URL)
	if err != nil {
		h.die(err.Error())
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(data, want) {
		h.die("published artifact bytes are unavailable or changed")
	}
}
