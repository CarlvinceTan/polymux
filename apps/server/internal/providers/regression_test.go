package providers

import (
	"crypto/rand"
	"fmt"
	"runtime"
	"testing"
)

func TestKeychainIsolation(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS keychain scope")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("polymux-audit-%x", nonce)
	a, b := NewKeychain(t.TempDir()), NewKeychain(t.TempDir())
	if err := a.Set(id, "synthetic-audit-value-not-a-real-credential"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Delete(id) })
	if _, err := b.Get(id); err == nil {
		t.Fatal("a different throwaway data directory can read this instance's provider secret")
	}
}

func TestGenerationTags(t *testing.T) {
	p := Builtin("gemini")
	if p == nil {
		t.Fatal("Gemini builtin missing")
	}
	models, err := p.GetModels()
	if err != nil || len(models) == 0 {
		t.Fatal("model fixture unavailable", err)
	}
	previous := []Model{{ID: models[0].ID, Enabled: true, Tags: []string{"text", "image-gen"}}}
	for _, model := range Models(p, nil, previous) {
		if model.ID != previous[0].ID {
			continue
		}
		for _, tag := range model.Tags {
			if tag == "image-gen" {
				return
			}
		}
		t.Fatal("refresh discarded existing image-gen capability")
	}
	t.Fatal("refresh removed model")
}
