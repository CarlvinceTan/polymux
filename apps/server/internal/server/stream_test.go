package server

import (
	"context"
	"testing"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

func TestGroupStreamAfterPosts(t *testing.T) {
	ctx := context.WithValue(t.Context(), chainKey{}, &chainState{})
	state := &turnState{posts: 5}
	run := contract.Run{Activity: contract.RunActivityGenerating, Summary: "Composing reply"}
	message := contract.Message{}
	stream := newStream(nil, ctx, "a", store.Work{}, state, &run, &message)
	stream.lastFlush = time.Now()
	if err := stream.text(ctx, "Final result"); err != nil {
		t.Fatal(err)
	}
	if store.Text(message) != "Final result" || stream.pending.String() != "Final result" {
		t.Fatal("progress posts suppressed the final streamed reply", message)
	}
}

func TestStreamHeldPrefix(t *testing.T) {
	for _, c := range []struct {
		deltas []string
		want   string
	}{
		{[]string{"N", "ice work"}, "Nice work"},
		{[]string{"NO_", "RESPONSE"}, ""},
	} {
		ctx := context.WithValue(t.Context(), chainKey{}, &chainState{})
		run := contract.Run{Activity: contract.RunActivityGenerating, Summary: "Composing reply"}
		stream := newStream(nil, ctx, "a", store.Work{}, &turnState{}, &run, &contract.Message{})
		stream.lastFlush = time.Now()
		for _, delta := range c.deltas {
			if err := stream.text(ctx, delta); err != nil {
				t.Fatal(err)
			}
		}
		if stream.pending.String() != c.want {
			t.Fatalf("%q streamed %q, want %q", c.deltas, stream.pending.String(), c.want)
		}
	}
}
