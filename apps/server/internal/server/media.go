package server

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	"github.com/CarlvinceTan/polymux/apps/server/internal/settings"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

type mediaTool struct {
	server *Server
	files  *outputFiles
	kind   string
	client *http.Client
	key    func(context.Context, string, settings.Provider) (string, error)
}

type mediaJob struct {
	ID           string `json:"id"`
	Owner        string `json:"owner"`
	Conversation string `json:"conversation"`
	Kind         string `json:"kind"`
	Prompt       string `json:"prompt"`
	Provider     string `json:"provider"`
	Type         string `json:"type"`
	BaseURL      string `json:"baseURL"`
	Model        string `json:"model"`
	Status       string `json:"status"`
	Message      string `json:"message"`
	Call         string `json:"call"`
	Delivered    bool   `json:"delivered"`
	NextAt       int64  `json:"nextAt,omitempty"`
	Operation    string `json:"operation,omitempty"`
	Mime         string `json:"mime,omitempty"`
}

func (t *mediaTool) Name() string                         { return "generate_" + t.kind }
func (t *mediaTool) Label() string                        { return "Generate " + t.kind }
func (*mediaTool) ToolClass() string                      { return "consequential" }
func (*mediaTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (t *mediaTool) Schema() ai.ToolSchema {
	properties := map[string]any{"prompt": map[string]any{"type": "string", "minLength": 1, "maxLength": 4000}}
	required := []string{"prompt"}
	description := "Generate one image using the configured image-generation provider and attach it to the conversation. This may incur provider charges. Do not automatically resubmit failed or uncertain requests. A missing provider credential can be collected with request_secret using the configured provider ID and origin."
	if t.kind == "video" {
		properties["job_id"] = map[string]any{"type": "string", "description": "Job ID returned by an earlier call in this conversation. Poll instead of submitting the prompt again."}
		required = []string{}
		description = "Submit one video using the configured video-generation provider, or check an existing job. Supply exactly one of prompt or job_id. Pending jobs return immediately and finish in the background, including after restart; the completed video is attached here automatically. You may check job_id for status, never resubmit the prompt. This may incur provider charges. Do not automatically resubmit failed or uncertain requests."
	}
	return ai.ToolSchema{Name: t.Name(), Description: description, Parameters: map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}}
}

func (t *mediaTool) Execute(ctx context.Context, call string, raw json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	empty := agent.AgentToolResult{}
	var args struct {
		Prompt string `json:"prompt"`
		JobID  string `json:"job_id"`
	}
	if err := toolArgs(raw, &args); err != nil {
		return empty, err
	}
	scope, err := outputScope(ctx, call)
	if err != nil {
		return empty, err
	}
	state, err := scopedTurn(ctx, t.server.Store)
	if err != nil {
		return empty, err
	}
	if (args.Prompt == "") == (args.JobID == "") || (t.kind != "video" && args.JobID != "") {
		return empty, errors.New("provide a prompt, or a video job_id to check")
	}
	if args.Prompt != "" && (strings.TrimSpace(args.Prompt) == "" || !utf8.ValidString(args.Prompt) || utf8.RuneCountInString(args.Prompt) > 4000) {
		return empty, errors.New("prompt must contain 1 to 4000 UTF-8 characters")
	}
	owner, conversation := state.run.AgentID, state.run.ConversationID
	id := args.JobID
	if id == "" {
		sum := sha256.Sum256([]byte(owner + "\x00" + conversation + "\x00" + scope.messageID + "\x00" + call))
		id = hex.EncodeToString(sum[:16])
	}
	if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 16 {
		return empty, errors.New("invalid media job_id")
	}
	// All sessions share the journal lock, including calls recovered after a reconnect.
	t.files.jobs.Lock()
	defer t.files.jobs.Unlock()
	dir := filepath.Join(t.server.config.DataDir, "media-jobs")
	path := filepath.Join(dir, id+".json")
	job := mediaJob{}
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &job); err != nil {
			return empty, fmt.Errorf("read media job: %w", err)
		}
		if job.Owner != owner || job.Conversation != conversation || job.Kind != t.kind {
			return empty, errors.New("media job is unavailable in this conversation")
		}
		if job.ID != id || job.Message == "" || job.Call == "" || !slices.Contains([]string{"uncertain", "failed", "pending", "ready"}, job.Status) {
			return empty, errors.New("media job journal is invalid; do not resubmit automatically")
		}
		if args.Prompt != "" && job.Prompt != args.Prompt {
			return empty, errors.New("media tool identity was reused with a different prompt")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	} else {
		if args.JobID != "" {
			return empty, errors.New("media job is unavailable in this conversation")
		}
		choices, err := t.candidates()
		if err != nil {
			return empty, err
		}
		// Moving past a model with no credential is safe: nothing has been submitted yet.
		var pick mediaChoice
		for _, choice := range choices {
			if _, err = t.headers(ctx, owner, choice.provider); err == nil {
				pick = choice
				break
			}
		}
		if pick.provider.ID == "" {
			return empty, err
		}
		provider, model := pick.provider, pick.model
		job = mediaJob{ID: id, Owner: owner, Conversation: conversation, Kind: t.kind, Prompt: args.Prompt,
			Provider: provider.ID, Type: provider.Type, BaseURL: strings.TrimRight(provider.BaseURL, "/"), Model: model, Status: "new", Message: scope.messageID, Call: call}
	}
	if job.Status == "uncertain" || job.Status == "failed" {
		return empty, fmt.Errorf("media job %s is %s; do not resubmit automatically", job.ID, job.Status)
	}
	if job.Status == "ready" {
		return t.deliver(ctx, dir, &job)
	}
	provider, err := t.jobProvider(job)
	if err != nil {
		return empty, err
	}
	headers, err := t.headers(ctx, owner, provider)
	if err != nil {
		return empty, err
	}
	if job.Status == "new" {
		// A crash or lost response after submission must never create a second paid job.
		job.Status = "uncertain"
		if err := saveMediaJob(path, job); err != nil {
			return empty, err
		}
		if t.kind == "image" {
			image, mediaType, err := t.image(ctx, job, headers)
			if err != nil {
				return empty, fmt.Errorf("image job %s could not complete; do not resubmit automatically: %w", id, err)
			}
			if err := saveMediaBytes(filepath.Join(dir, id+".bin"), image); err != nil {
				return empty, err
			}
			job.Status, job.Mime = "ready", mediaType
		} else {
			operation, err := t.submitVideo(ctx, job, headers)
			if err != nil {
				return empty, fmt.Errorf("video job %s could not submit; do not resubmit automatically: %w", id, err)
			}
			job.Status, job.Operation = "pending", operation
			job.NextAt = time.Now().Add(10 * time.Second).Unix()
		}
		if err := saveMediaJob(path, job); err != nil {
			return empty, err
		}
	} else if job.Status == "pending" && args.JobID != "" {
		if err := t.advance(ctx, dir, &job, headers); err != nil {
			return empty, fmt.Errorf("check video job %s: %w", id, err)
		}
	}
	if job.Status == "ready" {
		return t.deliver(ctx, dir, &job)
	}
	if job.Status == "failed" {
		return empty, errors.New("video generation finished without a video; do not resubmit automatically")
	}
	data, _ = json.Marshal(map[string]string{"job_id": id, "status": job.Status})
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "Video is pending and will be attached here automatically. To check status use job_id " + id + "; do not submit the prompt again."}}, StructuredContent: data}, nil
}

func (t *mediaTool) advance(ctx context.Context, dir string, job *mediaJob, headers http.Header) error {
	uri, done, err := t.pollVideo(ctx, *job, headers)
	if err != nil {
		return err
	}
	job.NextAt = time.Now().Add(10 * time.Second).Unix()
	if done {
		if uri == "" {
			job.Status = "failed"
		} else {
			video, err := t.download(ctx, job.BaseURL, uri, headers)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(http.DetectContentType(video), "video/") {
				return errors.New("video provider returned no valid video bytes")
			}
			if err := saveMediaBytes(filepath.Join(dir, job.ID+".bin"), video); err != nil {
				return err
			}
			job.Status, job.Mime = "ready", "video/mp4"
		}
	}
	return saveMediaJob(filepath.Join(dir, job.ID+".json"), *job)
}

type mediaChoice struct {
	provider settings.Provider
	model    string
}

// candidates lists the usable models in the order the settings give; auto takes every one in provider order.
func (t *mediaTool) candidates() ([]mediaChoice, error) {
	capability := t.kind + "-gen"
	snapshot := t.server.settings.Snapshot()
	order := snapshot.ImageOrder
	if t.kind == "video" {
		order = snapshot.VideoOrder
	}
	available := t.server.settings.Providers()
	slices.SortStableFunc(available, func(a, b settings.Provider) int { return cmp.Compare(a.Order, b.Order) })
	var found []mediaChoice
	var unsupported error
	consider := func(provider settings.Provider, model settings.Model) {
		if !provider.Enabled || !model.Enabled || !slices.Contains(model.Tags, capability) {
			return
		}
		if err := mediaAdapter(provider.Type, t.kind); err != nil {
			unsupported = err
			return
		}
		found = append(found, mediaChoice{provider, model.ID})
	}
	if order.Mode == contract.MediaOrderModeCustom {
		for _, step := range order.Models {
			for _, provider := range available {
				for _, model := range provider.Models {
					if provider.ID == step.Provider && model.ID == step.Model {
						consider(provider, model)
					}
				}
			}
		}
	} else {
		for _, provider := range available {
			for _, model := range provider.Models {
				consider(provider, model)
			}
		}
	}
	if len(found) > 0 {
		return found, nil
	}
	if unsupported != nil {
		return nil, unsupported
	}
	return nil, fmt.Errorf("no enabled %s provider/model is configured for native generation", capability)
}

func mediaAdapter(provider, kind string) error {
	if slices.Contains(providers.Generates[provider], kind+"-gen") {
		return nil
	}
	return fmt.Errorf("native %s generation is not supported for provider type %s", kind, provider)
}

func (t *mediaTool) jobProvider(job mediaJob) (settings.Provider, error) {
	for _, provider := range t.server.settings.Providers() {
		if provider.ID == job.Provider && provider.Enabled && provider.Type == job.Type && strings.TrimRight(provider.BaseURL, "/") == job.BaseURL {
			if _, err := credentialOrigin(job.BaseURL); err != nil {
				return settings.Provider{}, err
			}
			return provider, mediaAdapter(job.Type, job.Kind)
		}
	}
	return settings.Provider{}, errors.New("media job provider changed or is disabled; restore its original provider configuration to check this job")
}

func (t *mediaTool) headers(ctx context.Context, owner string, provider settings.Provider) (http.Header, error) {
	headers := make(http.Header)
	for name, value := range provider.Headers {
		headers.Set(name, value)
	}
	if headers.Get("Authorization") != "" || (provider.Type == "gemini" && headers.Get("x-goog-api-key") != "") {
		return headers, nil
	}
	var key string
	var err error
	if t.key != nil {
		key, err = t.key(ctx, owner, provider)
	} else {
		key, err = providers.NewKeychain(filepath.Join(t.server.config.DataDir, "provider-keys")).Get(provider.ID)
		if err != nil || key == "" {
			key, err = t.server.credential(ctx, owner, provider.ID, provider.BaseURL)
		}
	}
	if err != nil || strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("credential %s is unavailable; use request_secret for origin %s", provider.ID, provider.BaseURL)
	}
	if provider.Type == "gemini" {
		headers.Set("x-goog-api-key", key)
	} else {
		headers.Set("Authorization", "Bearer "+key)
	}
	return headers, nil
}

func (t *mediaTool) request(ctx context.Context, method, endpoint string, headers http.Header, body any, limit int64) ([]byte, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, input)
	if err != nil {
		return nil, errors.New("invalid media provider endpoint")
	}
	req.Header = headers.Clone()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{Timeout: 5 * time.Minute}
	if t.client != nil {
		client = *t.client
	}
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many media redirects")
		}
		if next.URL.Scheme != via[0].URL.Scheme || next.URL.Host != via[0].URL.Host {
			// A signed public download may redirect, but provider credentials must not.
			next.Header = make(http.Header)
			if next.URL.Scheme != "https" {
				return errors.New("media download redirect requires HTTPS")
			}
		}
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("media provider request did not complete")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// Provider bodies may echo request headers or credentials.
		return nil, fmt.Errorf("media provider returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("media response is incomplete or exceeds the output limit")
	}
	return data, nil
}

func (t *mediaTool) image(ctx context.Context, job mediaJob, headers http.Header) ([]byte, string, error) {
	body := map[string]any{"model": job.Model, "prompt": job.Prompt, "n": 1, "output_format": "png"}
	endpoint := job.BaseURL + "/images/generations"
	if job.Type == "gemini" {
		endpoint = job.BaseURL + "/models/" + url.PathEscape(strings.TrimPrefix(job.Model, "models/")) + ":generateContent"
		body = map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]string{"text": job.Prompt}}}}, "generationConfig": map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}}
	}
	data, err := t.request(ctx, http.MethodPost, endpoint, headers, body, 180<<20)
	if err != nil {
		return nil, "", err
	}
	var encoded string
	if job.Type == "openai" {
		var response struct {
			Data []struct {
				Base64 string `json:"b64_json"`
			} `json:"data"`
		}
		if json.Unmarshal(data, &response) != nil || len(response.Data) == 0 {
			return nil, "", errors.New("image provider returned no image")
		}
		encoded = response.Data[0].Base64
	} else {
		var response struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Thought bool `json:"thought"`
						Inline  struct {
							Mime string `json:"mimeType"`
							Data string `json:"data"`
						} `json:"inlineData"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, "", errors.New("image provider returned invalid JSON")
		}
		for _, candidate := range response.Candidates {
			for _, part := range candidate.Content.Parts {
				if !part.Thought && strings.HasPrefix(part.Inline.Mime, "image/") && part.Inline.Data != "" {
					encoded = part.Inline.Data
					break
				}
			}
			if encoded != "" {
				break
			}
		}
	}
	image, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(image) == 0 || len(image) > outputLimit || !strings.HasPrefix(http.DetectContentType(image), "image/") {
		return nil, "", errors.New("image provider returned no valid image bytes")
	}
	return image, strings.Split(http.DetectContentType(image), ";")[0], nil
}

func (t *mediaTool) submitVideo(ctx context.Context, job mediaJob, headers http.Header) (string, error) {
	endpoint := job.BaseURL + "/models/" + url.PathEscape(strings.TrimPrefix(job.Model, "models/")) + ":predictLongRunning"
	body := map[string]any{"instances": []any{map[string]string{"prompt": job.Prompt}}}
	data, err := t.request(ctx, http.MethodPost, endpoint, headers, body, 1<<20)
	if err != nil {
		return "", err
	}
	var response struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &response) != nil || !mediaOperation(response.Name) {
		return "", errors.New("video provider returned no valid operation name")
	}
	return response.Name, nil
}

func mediaOperation(name string) bool {
	if name == "" || strings.ContainsAny(name, "?#%\\:") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (t *mediaTool) pollVideo(ctx context.Context, job mediaJob, headers http.Header) (string, bool, error) {
	if !mediaOperation(job.Operation) {
		return "", false, errors.New("invalid saved video operation")
	}
	data, err := t.request(ctx, http.MethodGet, job.BaseURL+"/"+job.Operation, headers, nil, 1<<20)
	if err != nil {
		return "", false, err
	}
	var response struct {
		Done     bool            `json:"done"`
		Error    json.RawMessage `json:"error"`
		Response struct {
			Generated struct {
				Samples []struct {
					Video struct {
						URI string `json:"uri"`
					} `json:"video"`
				} `json:"generatedSamples"`
			} `json:"generateVideoResponse"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", false, errors.New("video provider returned invalid JSON")
	}
	if !response.Done {
		return "", false, nil
	}
	if (len(response.Error) > 0 && string(response.Error) != "null") || len(response.Response.Generated.Samples) == 0 {
		return "", true, nil
	}
	return response.Response.Generated.Samples[0].Video.URI, true, nil
}

func (t *mediaTool) download(ctx context.Context, base, uri string, headers http.Header) ([]byte, error) {
	origin, err := url.Parse(base)
	if err != nil {
		return nil, errors.New("invalid media provider origin")
	}
	target, err := url.Parse(uri)
	if err != nil || target.User != nil || target.Fragment != "" {
		return nil, errors.New("invalid video download URL")
	}
	if target.Scheme != origin.Scheme || target.Host != origin.Host {
		return nil, errors.New("video download must begin at the configured provider origin")
	}
	return t.request(ctx, http.MethodGet, target.String(), headers, nil, outputLimit)
}

func (t *mediaTool) deliver(ctx context.Context, dir string, job *mediaJob) (agent.AgentToolResult, error) {
	if a, _, err := t.files.store.Attachment(ctx, outputID(job.Message, job.Call)); err == nil {
		job.Delivered = true
		if err := saveMediaJob(filepath.Join(dir, job.ID+".json"), *job); err != nil {
			return agent.AgentToolResult{}, err
		}
		return outputReceipt(a), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return agent.AgentToolResult{}, err
	}
	f, err := os.Open(filepath.Join(dir, job.ID+".bin"))
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("read generated media: %w", err)
	}
	defer f.Close()
	extension := ".png"
	switch job.Mime {
	case "image/jpeg":
		extension = ".jpg"
	case "image/webp":
		extension = ".webp"
	case "video/mp4":
		extension = ".mp4"
	}
	scope, err := outputScope(ctx, job.Call)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	copyScope := *scope
	copyScope.messageID = job.Message
	ctx = context.WithValue(ctx, htmlKey{}, &copyScope)
	result, err := t.files.publish(ctx, job.Call, "Generated "+job.Kind+extension, job.Mime, f)
	if err != nil {
		return result, err
	}
	job.Delivered = true
	return result, saveMediaJob(filepath.Join(dir, job.ID+".json"), *job)
}

func saveMediaJob(path string, job mediaJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return saveMediaBytes(path, data)
}

func saveMediaBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".media-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
