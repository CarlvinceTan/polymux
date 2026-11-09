package vault_test

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/CarlvinceTan/polymux/packages/vault"
)

// memoryKeys is only for examples/tests, not persistent unattended use.
type memoryKeys struct{ key []byte }

func (k *memoryKeys) Load() ([]byte, error) { return slices.Clone(k.key), nil }
func (k *memoryKeys) Save(key []byte) error { k.key = slices.Clone(key); return nil }
func (*memoryKeys) Protection() string      { return "example memory" }

func ExampleOpen() {
	dir, err := os.MkdirTemp("", "vault-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	ctx := context.Background()
	store, err := vault.Open(ctx, dir, &memoryKeys{})
	if err != nil {
		panic(err)
	}
	defer store.Close()
	item, err := store.Save(ctx, vault.Item{
		ID: "service", Kind: "api", Title: "Example service", URL: "https://example.com",
		Fields: map[string]string{"token": "synthetic-example-token"}, Codes: []vault.Code{},
	})
	if err != nil {
		panic(err)
	}
	metadata := vault.Summarize(item)
	fmt.Println(metadata.Title, metadata.Fields, item.Revision)
	// Output: Example service [token] 1
}
