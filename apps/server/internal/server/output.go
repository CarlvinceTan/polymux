package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
)

const outputLimit = 128 << 20

type outputFiles struct {
	store *store.Store
	dir   string
	mu    sync.Mutex
	jobs  sync.Mutex
}

type attachTool struct{ files *outputFiles }

func (*attachTool) Name() string                           { return "attach_file" }
func (*attachTool) Label() string                          { return "Attach file" }
func (*attachTool) ToolClass() string                      { return "reversible" }
func (*attachTool) ExecutionMode() agent.ToolExecutionMode { return agent.ToolModeSequential }
func (*attachTool) Schema() ai.ToolSchema {
	return ai.ToolSchema{Name: "attach_file", Description: "Deliver a generated workspace file to the user as a downloadable attachment. Relative paths use your current workspace. Copies one regular file up to 128 MB; the source remains available. Use an optional readable filename. The tool returns a receipt, so do not paste binary content into chat.", Parameters: map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
			"name": map[string]any{"type": "string", "maxLength": 255},
		}, "required": []string{"path"},
	}}
}

func (t *attachTool) Execute(ctx context.Context, call string, raw json.RawMessage, _ agent.ToolUpdateCallback) (agent.AgentToolResult, error) {
	var args struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := toolArgs(raw, &args); err != nil {
		return agent.AgentToolResult{}, err
	}
	scope, err := outputScope(ctx, call)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	if strings.TrimSpace(args.Path) == "" || len(args.Path) > 4096 || !utf8.ValidString(args.Path) {
		return agent.AgentToolResult{}, errors.New("provide a file path up to 4096 characters")
	}
	path := args.Path
	if !filepath.IsAbs(path) {
		if scope.cwd == "" {
			return agent.AgentToolResult{}, errors.New("workspace is unavailable; provide an absolute file path")
		}
		path = filepath.Join(scope.cwd, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return agent.AgentToolResult{}, fmt.Errorf("open output file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return agent.AgentToolResult{}, errors.New("output must be a regular file")
	}
	if info.Size() > outputLimit {
		return agent.AgentToolResult{}, errors.New("output file exceeds 128 MB")
	}
	if args.Name == "" {
		args.Name = filepath.Base(path)
	}
	return t.files.publish(ctx, call, args.Name, "", f)
}

func toolArgs(raw json.RawMessage, args any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(args); err != nil {
		return fmt.Errorf("read tool arguments: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("tool arguments must contain one object")
	}
	return nil
}

func outputScope(ctx context.Context, call string) (*htmlScope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scope, _ := ctx.Value(htmlKey{}).(*htmlScope)
	if scope == nil || scope.publish == nil || scope.messageID == "" || call == "" {
		return nil, errors.New("file delivery requires an active reply and tool call identity")
	}
	return scope, nil
}

func (f *outputFiles) publish(ctx context.Context, call, name, mediaType string, source io.Reader) (agent.AgentToolResult, error) {
	empty := agent.AgentToolResult{}
	scope, err := outputScope(ctx, call)
	if err != nil {
		return empty, err
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return empty, errors.New("filename must contain up to 255 printable characters")
	}
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == ".." || name == "/" {
		return empty, errors.New("provide a filename")
	}
	tmp, err := os.CreateTemp(f.dir, ".output-")
	if err != nil {
		return empty, fmt.Errorf("create output: %w", err)
	}
	defer os.Remove(tmp.Name())
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(tmp, digest), io.LimitReader(source, outputLimit+1))
	if err := errors.Join(copyErr, tmp.Close()); err != nil {
		return empty, fmt.Errorf("copy output: %w", err)
	}
	if size > outputLimit {
		return empty, errors.New("output file exceeds 128 MB")
	}
	if mediaType == "" {
		mediaType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if mediaType == "" {
			input, err := os.Open(tmp.Name())
			if err != nil {
				return empty, err
			}
			prefix := make([]byte, 512)
			n, _ := input.Read(prefix)
			input.Close()
			mediaType = http.DetectContentType(prefix[:n])
		}
	}
	mediaType = strings.Split(mediaType, ";")[0]
	id := outputID(scope.messageID, call)
	path := filepath.Join(f.dir, id)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	a, stored, err := f.store.Attachment(ctx, id)
	if err == nil {
		file, err := os.Open(stored)
		if err != nil {
			return empty, err
		}
		storedDigest := sha256.New()
		_, readErr := io.Copy(storedDigest, file)
		if err := errors.Join(readErr, file.Close()); err != nil {
			return empty, err
		}
		if a.Name != name || a.MimeType != mediaType || a.Size != size || !bytes.Equal(digest.Sum(nil), storedDigest.Sum(nil)) {
			return empty, errors.New("file tool identity was reused with different content")
		}
		if err := scope.publish(ctx, a, stored); err != nil {
			return empty, err
		}
		return outputReceipt(a), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	if previous, err := os.Open(path); err == nil {
		storedDigest := sha256.New()
		_, copyErr := io.Copy(storedDigest, previous)
		if err := errors.Join(copyErr, previous.Close()); err != nil {
			return empty, err
		}
		if !bytes.Equal(digest.Sum(nil), storedDigest.Sum(nil)) {
			return empty, errors.New("unfinished file identity has different content")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return empty, err
	} else if err := os.Rename(tmp.Name(), path); err != nil {
		return empty, fmt.Errorf("save output: %w", err)
	}
	kind := "file"
	for _, candidate := range []string{"image", "video", "audio"} {
		if strings.HasPrefix(mediaType, candidate+"/") {
			kind = candidate
		}
	}
	a = contract.Attachment{ID: id, Kind: kind, Name: name, MimeType: mediaType, Size: size, URL: "/attachments/" + id + "/content", IndexingState: contract.AttachmentIndexingStatePending}
	if kind == "image" || kind == "video" {
		if makeThumbnail(ctx, path, path+".jpg", kind) == nil {
			url := "/attachments/" + id + "/thumbnail"
			a.ThumbnailURL = &url
		}
	}
	if err := scope.publish(ctx, a, path); err != nil {
		os.Remove(path)
		os.Remove(path + ".jpg")
		return empty, fmt.Errorf("deliver output: %w", err)
	}
	return outputReceipt(a), nil
}

func outputReceipt(a contract.Attachment) agent.AgentToolResult {
	data, _ := json.Marshal(map[string]any{"attachmentID": a.ID, "name": a.Name, "kind": a.Kind, "size": a.Size})
	return agent.AgentToolResult{Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "Attached " + a.Name + " to the conversation."}}, StructuredContent: data}
}

func outputID(message, call string) string {
	sum := sha256.Sum256([]byte("output\x00" + message + "\x00" + call))
	return hex.EncodeToString(sum[:16])
}
