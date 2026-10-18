package agentruntime

import (
	"context"
	"reflect"
	"sync"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/durable"
)

const historyBudget = 32 << 20

// historyStorage retains decoded immutable entries for each conversation's active range.
type historyStorage struct {
	durable.Storage
	mu        sync.Mutex
	histories map[durable.ConversationId]historyRange
	bytes     int64
	clock     uint64
}

type historyRange struct {
	min       durable.EntryId
	tail      durable.EntryId
	entries   []durable.EntryRecord
	bytes     int64
	used      uint64
	oversized bool
}

func cacheHistory(storage durable.Storage) *historyStorage {
	return &historyStorage{Storage: storage, histories: make(map[durable.ConversationId]historyRange)}
}

func (s *historyStorage) Close(ctx context.Context) error {
	s.mu.Lock()
	s.histories = nil
	s.bytes = 0
	s.mu.Unlock()
	return s.Storage.Close(ctx)
}

func (s *historyStorage) ScanEntries(ctx context.Context, query durable.EntryQuery, limit int, cursor durable.Cursor) (durable.Page[durable.EntryRecord, durable.Cursor], error) {
	var after int64
	if cursor != nil {
		var ok bool
		after, ok = cursor["historyAfter"].(int64)
		if !ok || len(cursor) != 1 {
			return s.Storage.ScanEntries(ctx, query, limit, cursor)
		}
	}
	// Our cursor belongs to the wrapper, never to the underlying storage.
	passthrough := func() (durable.Page[durable.EntryRecord, durable.Cursor], error) {
		if cursor != nil {
			upper := durable.EntryId(after - 1)
			if query.MaxEntryId == nil || upper < *query.MaxEntryId {
				query.MaxEntryId = &upper
			}
			cursor = nil
		}
		return s.Storage.ScanEntries(ctx, query, limit, cursor)
	}
	if query.MaxEntryId == nil || limit <= 0 {
		return passthrough()
	}
	if err := ctx.Err(); err != nil {
		return durable.Page[durable.EntryRecord, durable.Cursor]{}, err
	}
	lower := durable.EntryId(0)
	if query.MinEntryId != nil {
		lower = *query.MinEntryId
	}
	s.mu.Lock()
	if s.histories == nil {
		s.mu.Unlock()
		return passthrough()
	}
	cached, exists := s.histories[query.ConversationId]
	if exists {
		s.clock++
		cached.used = s.clock
		s.histories[query.ConversationId] = cached
	}
	s.mu.Unlock()
	if !exists || cached.min != lower || cached.tail > *query.MaxEntryId {
		cached = historyRange{min: lower, entries: []durable.EntryRecord{}}
		exists = false
	}
	if cached.oversized {
		return passthrough()
	}
	if !exists || cached.tail != *query.MaxEntryId {
		suffix := query
		if cached.tail > 0 {
			suffix.MinEntryId = new(max(lower, cached.tail+1))
		}
		var added []durable.EntryRecord
		var next durable.Cursor
		for {
			if err := ctx.Err(); err != nil {
				return durable.Page[durable.EntryRecord, durable.Cursor]{}, err
			}
			page, err := s.Storage.ScanEntries(ctx, suffix, 256, next)
			if err != nil {
				return durable.Page[durable.EntryRecord, durable.Cursor]{}, err
			}
			for _, entry := range page.Items {
				cached.bytes += historySize(reflect.ValueOf(entry))
			}
			if cached.bytes > historyBudget {
				cached.tail = *query.MaxEntryId
				s.retainHistory(query.ConversationId, cached)
				return passthrough()
			}
			added = append(added, page.Items...)
			if page.Next == nil {
				break
			}
			next = *page.Next
		}
		cached.entries = append(added, cached.entries...)
		cached.tail = *query.MaxEntryId
		// Future cutoffs can gain entries without changing the requested bound.
		if len(cached.entries) > 0 && cached.entries[0].Id == cached.tail {
			s.retainHistory(query.ConversationId, cached)
		}
	}
	start := 0
	if cursor != nil {
		for start < len(cached.entries) && int64(cached.entries[start].Id) >= after {
			start++
		}
	}
	count := min(limit, len(cached.entries)-start)
	items := make([]durable.EntryRecord, count)
	for i, entry := range cached.entries[start : start+count] {
		items[i] = copyEntry(entry)
	}
	result := durable.Page[durable.EntryRecord, durable.Cursor]{Items: items}
	if start+count < len(cached.entries) {
		next := durable.Cursor{"historyAfter": int64(items[count-1].Id)}
		result.Next = &next
	}
	return result, nil
}

func (s *historyStorage) retainHistory(id durable.ConversationId, cached historyRange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.histories == nil {
		return
	}
	s.bytes -= s.histories[id].bytes
	delete(s.histories, id)
	// Remember oversized ranges so later pages stream directly from storage.
	if cached.bytes > historyBudget {
		cached = historyRange{min: cached.min, tail: cached.tail, oversized: true}
	}
	for len(s.histories) >= 64 || s.bytes+cached.bytes > historyBudget {
		var oldest durable.ConversationId
		used := ^uint64(0)
		for key, entry := range s.histories {
			if entry.used < used {
				oldest, used = key, entry.used
			}
		}
		s.bytes -= s.histories[oldest].bytes
		delete(s.histories, oldest)
	}
	s.clock++
	cached.used = s.clock
	s.histories[id] = cached
	s.bytes += cached.bytes
}

// Estimate retained decoded bytes once per append, including nested payloads.
func historySize(value reflect.Value) int64 {
	if !value.IsValid() {
		return 0
	}
	size := int64(value.Type().Size())
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !value.IsNil() {
			size += historySize(value.Elem())
		}
	case reflect.String:
		size += int64(value.Len())
	case reflect.Array, reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			size += historySize(value.Index(i))
		}
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			size += 64 + historySize(iter.Key()) + historySize(iter.Value())
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			size += historySize(value.Field(i))
		}
	}
	return size
}

func copyEntry(entry durable.EntryRecord) durable.EntryRecord {
	entry.Model = copyMessages(entry.Model)
	entry.Data = copyJSON(entry.Data)
	if entry.Head != nil {
		entry.Head = new(*entry.Head)
	}
	if entry.ByTaskId != nil {
		entry.ByTaskId = new(*entry.ByTaskId)
	}
	if entry.Edits != nil {
		edits := make([]durable.ContextEdit, len(entry.Edits))
		for i, edit := range entry.Edits {
			edit.Messages = copyMessages(edit.Messages)
			edits[i] = edit
		}
		entry.Edits = edits
	}
	return entry
}

func copyMessages(messages []ai.Message) []ai.Message {
	if messages == nil {
		return nil
	}
	copied := make([]ai.Message, len(messages))
	for i, message := range messages {
		// Plain user text has no nested mutable fields.
		if user, ok := message.(ai.UserMessage); ok {
			if _, ok := user.Content.(ai.UserText); ok || user.Content == nil {
				copied[i] = user
				continue
			}
		}
		if message != nil {
			copied[i] = copyHistory(reflect.ValueOf(message)).Interface().(ai.Message)
		}
	}
	return copied
}

func copyJSON(value any) any {
	switch typed := value.(type) {
	case nil, bool, string, float64, int, int64:
		return value
	case map[string]any:
		if typed == nil {
			return typed
		}
		copied := make(map[string]any, len(typed))
		for key, item := range typed {
			copied[key] = copyJSON(item)
		}
		return copied
	case []any:
		if typed == nil {
			return typed
		}
		copied := make([]any, len(typed))
		for i, item := range typed {
			copied[i] = copyJSON(item)
		}
		return copied
	default:
		return copyHistory(reflect.ValueOf(value)).Interface()
	}
}

// Storage reads must be detached, including nested tool arguments and message content.
func copyHistory(value reflect.Value) reflect.Value {
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.New(value.Type()).Elem()
		if value.Kind() == reflect.Pointer {
			copied.Set(reflect.New(value.Type().Elem()))
			copied.Elem().Set(copyHistory(value.Elem()))
		} else {
			copied.Set(copyHistory(value.Elem()))
		}
		return copied
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			copied.Index(i).Set(copyHistory(value.Index(i)))
		}
		return copied
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type())
		}
		copied := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			copied.SetMapIndex(iter.Key(), copyHistory(iter.Value()))
		}
		return copied
	case reflect.Struct:
		copied := reflect.New(value.Type()).Elem()
		copied.Set(value)
		for i := 0; i < value.NumField(); i++ {
			if copied.Field(i).CanSet() && value.Field(i).CanInterface() {
				copied.Field(i).Set(copyHistory(value.Field(i)))
			}
		}
		return copied
	case reflect.Array:
		copied := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			copied.Index(i).Set(copyHistory(value.Index(i)))
		}
		return copied
	default:
		return value
	}
}
