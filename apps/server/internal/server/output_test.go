package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

func outputContext(t *testing.T, api *Server, message, cwd string) (context.Context, *[]contract.Attachment) {
	t.Helper()
	attachments := &[]contract.Attachment{}
	scope := &htmlScope{messageID: message, cwd: cwd, publish: func(ctx context.Context, a contract.Attachment, path string) error {
		*attachments = append(*attachments, a)
		return api.Store.SaveAttachment(ctx, a, path)
	}}
	return context.WithValue(t.Context(), htmlKey{}, scope), attachments
}

func TestAttachFile(t *testing.T) {
	api, _ := startTest(t, &voiceFactory{})
	cwd := t.TempDir()
	path := filepath.Join(cwd, "report.txt")
	if err := os.WriteFile(path, []byte("The amber report."), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, attachments := outputContext(t, api, "reply", cwd)
	tool := &attachTool{files: api.outputs}
	args := json.RawMessage(`{"path":"report.txt","name":"Weekly report.txt"}`)
	for range 2 {
		result, err := tool.Execute(ctx, "file-call", args, nil)
		if err != nil || !strings.Contains(result.Text(), "Weekly report.txt") {
			t.Fatal("file not delivered", result, err)
		}
	}
	if len(*attachments) != 2 || (*attachments)[0].ID != (*attachments)[1].ID {
		t.Fatal("replay created a second artifact", *attachments)
	}
	a, saved, err := api.Store.Attachment(ctx, (*attachments)[0].ID)
	if err != nil || a.Kind != "file" || a.Name != "Weekly report.txt" {
		t.Fatal("attachment metadata", a, err)
	}
	data, err := os.ReadFile(saved)
	if err != nil || string(data) != "The amber report." || saved == path {
		t.Fatal("file snapshot incorrect", err)
	}
	if err := os.WriteFile(path, []byte("Changed report."), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Execute(ctx, "file-call", args, nil); err == nil {
		t.Fatal("immutable attachment identity accepted changed bytes")
	}
	if _, err := tool.Execute(ctx, "directory", json.RawMessage(`{"path":"."}`), nil); err == nil {
		t.Fatal("directory accepted as attachment")
	}
	if _, err := tool.Execute(t.Context(), "outside", args, nil); err == nil {
		t.Fatal("attachment accepted without reply context")
	}
	ctx, other := outputContext(t, api, "other-reply", cwd)
	if _, err := tool.Execute(ctx, "file-call", args, nil); err != nil || (*other)[0].ID == a.ID {
		t.Fatal("different replies share file identity", err)
	}
}
