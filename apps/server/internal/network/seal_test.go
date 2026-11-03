package network

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"io"
	"testing"
)

func sealed(t *testing.T, plain []byte) ([]byte, *ecdh.PrivateKey, string) {
	t.Helper()
	target, err := NewMoveKey()
	if err != nil {
		t.Fatal(err)
	}
	sender, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := sealCipher(sender, publicKey(target))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	seal := &sealer{out: &out, aead: aead}
	if _, err := seal.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := seal.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes(), target, publicKey(sender)
}

func open(t *testing.T, data []byte, key *ecdh.PrivateKey, public string) ([]byte, error) {
	t.Helper()
	in, err := NewOpener(bytes.NewReader(data), key, public)
	if err != nil {
		t.Fatal(err)
	}
	return io.ReadAll(in)
}

func TestSealedStreamRoundTrips(t *testing.T) {
	for _, size := range []int{0, 1, sealSize, sealSize*2 + 7} {
		plain := make([]byte, size)
		if _, err := rand.Read(plain); err != nil {
			t.Fatal(err)
		}
		data, key, public := sealed(t, plain)
		// A byte or two can turn up in any ciphertext by chance, so only a longer plaintext shows it wasn't sealed.
		if size >= 16 && bytes.Contains(data, plain) {
			t.Fatal("the stream isn't sealed")
		}
		got, err := open(t, data, key, public)
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
}

func TestSealedStreamRefusesDamage(t *testing.T) {
	plain := make([]byte, sealSize*2+7)
	data, key, public := sealed(t, plain)
	altered := bytes.Clone(data)
	altered[len(altered)/2] ^= 1
	if _, err := open(t, altered, key, public); err == nil {
		t.Fatal("an altered stream opened")
	}
	// Whole frames dropped from the end still decrypt, so only the last-frame mark catches this.
	if _, err := open(t, data[:sealSize+64], key, public); err == nil {
		t.Fatal("a cut-off stream looked complete")
	}
	other, err := NewMoveKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := open(t, data, other, public); err == nil {
		t.Fatal("the wrong server opened the stream")
	}
}
