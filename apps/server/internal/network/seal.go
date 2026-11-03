package network

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
)

// sealSize is how much data each sealed frame holds.
const sealSize = 1 << 20

// A move is sealed to its target, because a tunnel in between may see past TLS and this
// stream holds every chat and the keys to the vault.

// NewMoveKey makes the target's half of a move. Its public part travels in a Check.
func NewMoveKey() (*ecdh.PrivateKey, error) { return ecdh.X25519().GenerateKey(rand.Reader) }

func publicKey(key *ecdh.PrivateKey) string {
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
}

func sealCipher(private *ecdh.PrivateKey, public string) (cipher.AEAD, error) {
	raw, err := base64.RawURLEncoding.DecodeString(public)
	if err != nil {
		return nil, err
	}
	peer, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, err
	}
	shared, err := private.ECDH(peer)
	if err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, shared, nil, "polymux move", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(aead cipher.AEAD, count uint64) []byte {
	value := make([]byte, aead.NonceSize())
	binary.BigEndian.PutUint64(value[len(value)-8:], count)
	return value
}

// sealer writes frames; the last one is marked, so a cut-off stream can't pass as complete.
type sealer struct {
	out   io.Writer
	aead  cipher.AEAD
	data  []byte
	count uint64
}

func (s *sealer) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		n := min(sealSize-len(s.data), len(p))
		s.data = append(s.data, p[:n]...)
		p = p[n:]
		if len(s.data) == sealSize {
			if err := s.frame(false); err != nil {
				return 0, err
			}
		}
	}
	return written, nil
}

func (s *sealer) Close() error { return s.frame(true) }

func (s *sealer) frame(last bool) error {
	sealed := s.aead.Seal(nil, nonce(s.aead, s.count), s.data, frameMark(last))
	s.count++
	s.data = s.data[:0]
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(sealed)))
	if _, err := s.out.Write(size[:]); err != nil {
		return err
	}
	_, err := s.out.Write(sealed)
	return err
}

func frameMark(last bool) []byte {
	if last {
		return []byte{1}
	}
	return []byte{0}
}

// Opener reads a sealed stream, failing on anything altered or cut short.
type Opener struct {
	in    io.Reader
	aead  cipher.AEAD
	data  []byte
	count uint64
	done  bool
}

// NewOpener opens what was sealed to key by the sender of public.
func NewOpener(in io.Reader, key *ecdh.PrivateKey, public string) (*Opener, error) {
	aead, err := sealCipher(key, public)
	if err != nil {
		return nil, err
	}
	return &Opener{in: in, aead: aead}, nil
}

func (o *Opener) Read(p []byte) (int, error) {
	for len(o.data) == 0 {
		if o.done {
			return 0, io.EOF
		}
		if err := o.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, o.data)
	o.data = o.data[n:]
	return n, nil
}

func (o *Opener) next() error {
	var size [4]byte
	if _, err := io.ReadFull(o.in, size[:]); err != nil {
		return errors.Join(io.ErrUnexpectedEOF, err)
	}
	length := binary.BigEndian.Uint32(size[:])
	if length > sealSize+uint32(o.aead.Overhead()) {
		return errors.New("sealed frame too large")
	}
	sealed := make([]byte, length)
	if _, err := io.ReadFull(o.in, sealed); err != nil {
		return errors.Join(io.ErrUnexpectedEOF, err)
	}
	mark := nonce(o.aead, o.count)
	o.count++
	plain, err := o.aead.Open(nil, mark, sealed, frameMark(false))
	if err != nil {
		if plain, err = o.aead.Open(nil, mark, sealed, frameMark(true)); err != nil {
			return errors.New("sealed data failed its check")
		}
		o.done = true
	}
	o.data = plain
	return nil
}
