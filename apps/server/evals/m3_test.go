package evals

import (
	"testing"

	"github.com/CarlvinceTan/polymux/apps/server/internal/memory"
)

func TestM3IdenticalWrites(t *testing.T) {
	root := t.TempDir()
	provider := memory.New(root)
	t.Cleanup(func() { provider.Close() })
	first, err := provider.Save(t.Context(), agentID, "chat", memory.Document{
		Name: "reply-style", Text: "Explicit preference: short replies", Source: "user-1",
	})
	if err != nil {
		fail(t, "M3", "first write: "+err.Error())
		return
	}
	second, err := provider.Save(t.Context(), agentID, "chat", memory.Document{
		Name: "response-preference", Text: first.Text, Source: "user-2",
	})
	if err != nil {
		fail(t, "M3", "second write: "+err.Error())
		return
	}
	if first != second || first.Revision == "" {
		fail(t, "M3", "identical write did not return the canonical record")
		return
	}
	if err := provider.Close(); err != nil {
		fail(t, "M3", "close: "+err.Error())
		return
	}
	provider = memory.New(root)
	documents, err := provider.List(t.Context(), agentID, "chat")
	if err != nil {
		fail(t, "M3", "recall: "+err.Error())
		return
	}
	if len(documents) != 1 || documents[0] != first {
		fail(t, "M3", "identical facts did not persist as one record")
		return
	}
	pass(t, "M3", "identical facts under different names persist and recall as one record")
}
