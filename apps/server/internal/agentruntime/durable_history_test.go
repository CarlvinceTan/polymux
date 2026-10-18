package agentruntime

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
	"github.com/MichaelKinsy/PiG/durable/harness"
)

type historyReads struct {
	durable.Storage
	decoded int
	bounded int
}

func (s *historyReads) ScanEntries(ctx context.Context, query durable.EntryQuery, limit int, cursor durable.Cursor) (durable.Page[durable.EntryRecord, durable.Cursor], error) {
	page, err := s.Storage.ScanEntries(ctx, query, limit, cursor)
	s.decoded += len(page.Items)
	if query.MaxEntryId != nil {
		s.bounded += len(page.Items)
	}
	return page, err
}

func TestHistorySuffix(t *testing.T) {
	storage, closeStorage, err := NewDurableStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	counted := &historyReads{Storage: storage}
	cached := cacheHistory(counted)
	const conversation durable.ConversationId = 1
	if _, err := cached.Commit(t.Context(), []durable.StorageWrite{durable.ConversationWrite{Value: durable.ConversationRecord{Id: conversation}}}); err != nil {
		t.Fatal(err)
	}
	appendEntry := func(id durable.EntryId, head *durable.EntryId) {
		t.Helper()
		_, err := cached.Commit(t.Context(), []durable.StorageWrite{durable.EntryWrite{Value: durable.EntryRecord{Id: id, ConversationId: conversation, Kind: "pi.user", Head: head, Model: []ai.Message{ai.UserMessage{Content: ai.UserText("original")}}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for id := durable.EntryId(2); id <= 601; id++ {
		appendEntry(id, nil)
	}
	read := func(tail durable.EntryId, head *durable.EntryRecord) durable.ContextView {
		t.Helper()
		bounds := &harness.ContextBounds{Tail: tail, Head: head}
		got, err := harness.DeriveContext(t.Context(), cached, conversation, bounds)
		if err != nil {
			t.Fatal(err)
		}
		want, err := harness.DeriveContext(t.Context(), storage, conversation, bounds)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("cached history differs")
		}
		return got
	}
	first := read(601, nil)
	if counted.decoded != 600 {
		t.Fatalf("decoded %d, want 600", counted.decoded)
	}
	first.Entries[0].Model[0] = ai.UserMessage{Content: ai.UserText("changed")}
	// Ephemeral conversations and forks must not evict an active agent's history.
	_, err = cached.Commit(t.Context(), []durable.StorageWrite{
		durable.ConversationWrite{Value: durable.ConversationRecord{Id: 1000}},
		durable.ConversationWrite{Value: durable.ConversationRecord{Id: 1001, Parent: &durable.ConversationParent{ConversationId: conversation, At: 601}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	read(601, nil)
	if counted.decoded != 600 {
		t.Fatal("unchanged history decoded again")
	}
	appendEntry(602, nil)
	read(602, nil)
	if counted.decoded != 601 {
		t.Fatalf("decoded %d, want only one appended entry", counted.decoded)
	}
	appendEntry(603, new(durable.EntryId(600)))
	marker, err := storage.Entry(t.Context(), 603)
	if err != nil {
		t.Fatal(err)
	}
	read(603, &marker.Entry)
	if counted.decoded != 605 {
		t.Fatalf("compaction decoded %d, want 605", counted.decoded)
	}
	read(601, nil)
	if counted.decoded != 1205 {
		t.Fatal("historical range did not invalidate")
	}

	_, err = cached.Commit(t.Context(), []durable.StorageWrite{durable.EntryWrite{Value: durable.EntryRecord{Id: 604, ConversationId: conversation, Kind: "edit", Edits: []durable.ContextEdit{{Target: 602, Action: durable.EditOmit}, {Target: 601, Action: durable.EditReplace, Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("replacement")}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	read(604, nil)
	appendEntry(605, new(durable.EntryId(605)))
	reset, err := storage.Entry(t.Context(), 605)
	if err != nil {
		t.Fatal(err)
	}
	view := read(605, &reset.Entry)
	if len(view.Entries) != 1 {
		t.Fatal("reset retained prior history")
	}
}

type historyGate struct {
	durable.Storage
	entered chan struct{}
	release chan struct{}
}

func (s *historyGate) ScanEntries(ctx context.Context, query durable.EntryQuery, limit int, cursor durable.Cursor) (durable.Page[durable.EntryRecord, durable.Cursor], error) {
	if query.ConversationId == 1 {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return durable.Page[durable.EntryRecord, durable.Cursor]{}, ctx.Err()
		}
	}
	return s.Storage.ScanEntries(ctx, query, limit, cursor)
}

func TestHistoryParallel(t *testing.T) {
	storage, closeStorage, err := NewDurableStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	gate := &historyGate{Storage: storage, entered: make(chan struct{}), release: make(chan struct{})}
	cached := cacheHistory(gate)
	_, err = cached.Commit(t.Context(), []durable.StorageWrite{
		durable.ConversationWrite{Value: durable.ConversationRecord{Id: 1}},
		durable.ConversationWrite{Value: durable.ConversationRecord{Id: 3}},
		durable.EntryWrite{Value: durable.EntryRecord{Id: 2, ConversationId: 1}},
		durable.EntryWrite{Value: durable.EntryRecord{Id: 4, ConversationId: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := cached.ScanEntries(ctx, durable.EntryQuery{ConversationId: 1, MaxEntryId: new(durable.EntryId(2))}, 256, nil)
		done <- err
	}()
	defer func() {
		close(gate.release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal("first scan did not start")
	}
	progress := make(chan error, 1)
	go func() {
		_, err := cached.Commit(ctx, []durable.StorageWrite{durable.ConversationWrite{Value: durable.ConversationRecord{Id: 5}}})
		if err == nil {
			_, err = cached.ScanEntries(ctx, durable.EntryQuery{ConversationId: 3, MaxEntryId: new(durable.EntryId(4))}, 256, nil)
		}
		progress <- err
	}()
	select {
	case err := <-progress:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("one conversation's scan blocked commits or another conversation")
	}
}

type historyCursor struct {
	durable.Storage
	cursor durable.Cursor
	query  durable.EntryQuery
}

func (s *historyCursor) ScanEntries(ctx context.Context, query durable.EntryQuery, limit int, cursor durable.Cursor) (durable.Page[durable.EntryRecord, durable.Cursor], error) {
	s.query, s.cursor = query, cursor
	return s.Storage.ScanEntries(ctx, query, limit, cursor)
}

func TestHistoryCursor(t *testing.T) {
	storage, closeStorage, err := NewDurableStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	tracked := &historyCursor{Storage: storage}
	cached := cacheHistory(tracked)
	_, err = cached.Commit(t.Context(), []durable.StorageWrite{
		durable.ConversationWrite{Value: durable.ConversationRecord{Id: 1}},
		durable.EntryWrite{Value: durable.EntryRecord{Id: 2, ConversationId: 1}},
		durable.EntryWrite{Value: durable.EntryRecord{Id: 3, ConversationId: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	query := durable.EntryQuery{ConversationId: 1, MaxEntryId: new(durable.EntryId(3))}
	page, err := cached.ScanEntries(t.Context(), query, 1, nil)
	if err != nil || page.Next == nil {
		t.Fatalf("first page: %v, %v", page, err)
	}
	query.MaxEntryId = nil
	next, err := cached.ScanEntries(t.Context(), query, 1, *page.Next)
	if err != nil || len(next.Items) != 1 || next.Items[0].Id != 2 {
		t.Fatalf("passthrough page: %v, %v", next, err)
	}
	if tracked.cursor != nil || tracked.query.MaxEntryId == nil || *tracked.query.MaxEntryId != 2 {
		t.Fatal("private cursor was not translated")
	}
	cached.retainHistory(1, historyRange{tail: 3, bytes: historyBudget + 1})
	query.MaxEntryId = new(durable.EntryId(3))
	next, err = cached.ScanEntries(t.Context(), query, 1, *page.Next)
	if err != nil || len(next.Items) != 1 || next.Items[0].Id != 2 || tracked.cursor != nil {
		t.Fatalf("oversized passthrough page: %v, %v", next, err)
	}
	if err := cached.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	query.MaxEntryId = new(durable.EntryId(3))
	if _, err := cached.ScanEntries(t.Context(), query, 1, *page.Next); err == nil {
		t.Fatal("closed storage accepted cursor")
	}
	if tracked.cursor != nil || *tracked.query.MaxEntryId != 2 {
		t.Fatal("closed cache leaked private cursor")
	}
}

func TestHistoryBounds(t *testing.T) {
	storage, closeStorage, err := NewDurableStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	cached := cacheHistory(storage)
	if _, err := cached.Commit(t.Context(), []durable.StorageWrite{durable.ConversationWrite{Value: durable.ConversationRecord{Id: 1}}}); err != nil {
		t.Fatal(err)
	}
	read := func(query durable.EntryQuery) {
		t.Helper()
		got, err := cached.ScanEntries(t.Context(), query, 256, nil)
		if err != nil {
			t.Fatal(err)
		}
		want, err := storage.ScanEntries(t.Context(), query, 256, nil)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("bounds: got %v, want %v, error %v", got, want, err)
		}
	}
	future := durable.EntryQuery{ConversationId: 1, MaxEntryId: new(durable.EntryId(10))}
	read(future)
	if _, err := cached.Commit(t.Context(), []durable.StorageWrite{durable.EntryWrite{Value: durable.EntryRecord{Id: 2, ConversationId: 1}}}); err != nil {
		t.Fatal(err)
	}
	read(future)
	read(durable.EntryQuery{ConversationId: 1, MinEntryId: new(durable.EntryId(5)), MaxEntryId: new(durable.EntryId(2))})
	if _, err := cached.Commit(t.Context(), []durable.StorageWrite{durable.EntryWrite{Value: durable.EntryRecord{Id: 6, ConversationId: 1}}}); err != nil {
		t.Fatal(err)
	}
	read(future)
	read(durable.EntryQuery{ConversationId: 1, MinEntryId: new(durable.EntryId(5)), MaxEntryId: new(durable.EntryId(6))})
}

func TestHistoryBudget(t *testing.T) {
	cached := cacheHistory(nil)
	cached.retainHistory(1, historyRange{bytes: historyBudget / 2})
	cached.retainHistory(2, historyRange{bytes: historyBudget / 2})
	cached.retainHistory(3, historyRange{bytes: historyBudget / 2})
	if _, ok := cached.histories[1]; ok || cached.bytes != historyBudget {
		t.Fatal("byte budget did not evict the oldest range")
	}
	cached.retainHistory(3, historyRange{bytes: historyBudget + 1, tail: 9})
	if !cached.histories[3].oversized || cached.histories[3].entries != nil || cached.bytes != historyBudget/2 {
		t.Fatal("oversized range retained decoded entries")
	}
	for id := durable.ConversationId(4); id <= 70; id++ {
		cached.retainHistory(id, historyRange{})
	}
	if len(cached.histories) != 64 {
		t.Fatal("conversation budget was exceeded")
	}
}

func TestDurableHistory(t *testing.T) {
	storage, closeStorage, err := NewDurableStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	counted := &historyReads{Storage: storage}
	runtime, err := NewDurable(t.Context(), t.TempDir(), "http://127.0.0.1:1/v1", "test", "test", Client{}, counted)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	session, err := runtime.Open(t.Context(), Definition{}, "")
	if err != nil {
		t.Fatal(err)
	}
	conversation := session.(*durableSession).conversation
	for i := 0; i < 3; i++ {
		_, err := conversation.Commit(t.Context(), func(tx durable.Tx) (any, error) {
			if _, err := tx.CreateConversation(durable.CreateConversationOptions{Ownership: durable.ConversationOwnership{Kind: durable.ConversationOwnerless}}); err != nil {
				return nil, err
			}
			return tx.AppendEntry(conversation.Id(), durable.EntryDraft{Kind: "pi.user", Model: []ai.Message{ai.UserMessage{Content: ai.UserText("next turn")}}})
		})
		if err != nil {
			t.Fatal(err)
		}
		before := counted.bounded
		view, err := conversation.Context(t.Context())
		if err != nil || len(view.Messages) < i+1 {
			t.Fatalf("context after append: %v, %v", view, err)
		}
		if i > 0 && counted.bounded-before != 1 {
			t.Fatalf("decoded %d history entries after append, want 1", counted.bounded-before)
		}
		before = counted.bounded
		if _, err := conversation.Context(t.Context()); err != nil {
			t.Fatal(err)
		}
		if counted.bounded != before {
			t.Fatal("durable runtime decoded unchanged history")
		}
	}
}

func TestHistoryDetached(t *testing.T) {
	storage, closeStorage, err := NewDurableStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer closeStorage()
	cached := cacheHistory(storage)
	original := durable.EntryRecord{
		Id: 2, ConversationId: 1, Kind: "custom", Head: new(durable.EntryId(2)), ByTaskId: new(durable.TaskId(99)),
		Data: map[string]any{"nested": []any{map[string]any{"text": "original"}}},
		Model: []ai.Message{
			ai.UserMessage{Content: ai.UserText("original")},
			ai.UserMessage{Content: ai.UserContentBlocks{ai.TextContent{Text: "original"}}},
			ai.AssistantMessage{Content: []ai.AssistantContentBlock{ai.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{"text": "original"}}}},
			ai.ToolResultMessage{ToolCallID: "call", ToolName: "tool", Content: []ai.ToolResultMessageContent{ai.TextContent{Text: "original"}}, Details: map[string]any{"text": "original"}},
		},
		Edits: []durable.ContextEdit{{Target: 2, Action: durable.EditReplace, Messages: []ai.Message{ai.UserMessage{Content: ai.UserText("original")}}}},
	}
	_, err = cached.Commit(t.Context(), []durable.StorageWrite{durable.ConversationWrite{Value: durable.ConversationRecord{Id: 1}}, durable.EntryWrite{Value: original}})
	if err != nil {
		t.Fatal(err)
	}
	query := durable.EntryQuery{ConversationId: 1, MaxEntryId: new(durable.EntryId(2))}
	first, err := cached.ScanEntries(t.Context(), query, 256, nil)
	if err != nil {
		t.Fatal(err)
	}
	*first.Items[0].Head = 99
	first.Items[0].Data.(map[string]any)["nested"].([]any)[0].(map[string]any)["text"] = "changed"
	first.Items[0].Model[0] = ai.UserMessage{Content: ai.UserText("changed")}
	first.Items[0].Model[1].(ai.UserMessage).Content.(ai.UserContentBlocks)[0] = ai.TextContent{Text: "changed"}
	first.Items[0].Model[2].(ai.AssistantMessage).Content[0].(ai.ToolCall).Arguments["text"] = "changed"
	first.Items[0].Model[3].(ai.ToolResultMessage).Details.(map[string]any)["text"] = "changed"
	first.Items[0].Edits[0].Messages[0] = ai.UserMessage{Content: ai.UserText("changed")}
	*first.Items[0].ByTaskId = 100
	second, err := cached.ScanEntries(t.Context(), query, 256, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := storage.ScanEntries(t.Context(), query, 256, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, want) {
		t.Fatal("caller changed cached values")
	}
	if err := cached.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := cached.ScanEntries(t.Context(), query, 256, nil); err == nil {
		t.Fatal("closed cache accepted a read")
	}
}
