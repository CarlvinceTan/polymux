package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/harness"
)

func BenchmarkDurableHistory(b *testing.B) {
	conversation := benchHistory(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		view, err := conversation.Context(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		if len(view.Messages) < 1024 {
			b.Fatal("history was lost")
		}
	}
}

func BenchmarkDurableHistoryAppend(b *testing.B) {
	conversation := benchHistory(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		_, err := conversation.Commit(b.Context(), func(tx durable.Tx) (any, error) {
			if _, err := tx.CreateConversation(durable.CreateConversationOptions{Ownership: durable.ConversationOwnership{Kind: durable.ConversationOwnerless}}); err != nil {
				return nil, err
			}
			return tx.AppendEntry(conversation.Id(), durable.EntryDraft{Kind: "pi.user", Model: []ai.Message{ai.UserMessage{Content: ai.UserText("next turn")}}})
		})
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		view, err := conversation.Context(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		if len(view.Messages) < 1025+i {
			b.Fatal("appended history was lost")
		}
	}
}

func benchHistory(b *testing.B) harness.Conversation {
	b.Helper()
	storage, closeStorage, err := NewDurableStorage(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = closeStorage() })
	runtime, err := NewDurable(b.Context(), b.TempDir(), "http://127.0.0.1:1/v1", "test", "test", Client{}, storage)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = runtime.Close() })
	session, err := runtime.Open(b.Context(), Definition{}, "")
	if err != nil {
		b.Fatal(err)
	}
	conversation := session.(*durableSession).conversation
	_, err = conversation.Commit(b.Context(), func(tx durable.Tx) (any, error) {
		for i := 0; i < 1024; i++ {
			_, err := tx.AppendEntry(conversation.Id(), durable.EntryDraft{Kind: "pi.user", Model: []ai.Message{ai.UserMessage{Content: ai.UserText(fmt.Sprintf("%d %s", i, strings.Repeat("history ", 128)))}}})
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		b.Fatal(err)
	}
	// Context uses the same history path as durable generation without provider latency.
	if _, err := conversation.Context(context.Background()); err != nil {
		b.Fatal(err)
	}
	return conversation
}
