package memory_test

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/CarlvinceTan/polymux/packages/memory"
)

func ExampleNew() {
	dir, err := os.MkdirTemp("", "memory-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	ctx := context.Background()
	store := memory.New(dir)
	_, err = store.Save(ctx, "assistant", "conversation", memory.Document{Name: "style", Text: "Short replies", Source: "user"})
	if err != nil {
		panic(err)
	}
	if err := store.Close(); err != nil {
		panic(err)
	}
	store = memory.New(dir)
	defer store.Close()
	documents, err := store.List(ctx, "assistant", "conversation")
	if err != nil || len(documents) != 1 {
		panic("memory did not survive reopening")
	}
	fmt.Println(documents[0].Text)
	_, err = store.Save(ctx, "assistant", "conversation", memory.Document{Name: "style", Text: "A stale overwrite"})
	fmt.Println(errors.Is(err, memory.ErrConflict))
	// Output:
	// Short replies
	// true
}
