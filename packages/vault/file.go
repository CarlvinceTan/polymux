package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
)

// KDF settings for vaults this package creates; tests lower them.
var (
	kdfMemory     uint64 = 64 << 20
	kdfIterations uint64 = 3
)

// stamp identifies a version of a file on disk without reading it.
type stamp struct {
	size int64
	mod  time.Time
}

func stampOf(path string) (stamp, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return stamp{}, false
	}
	return stamp{size: info.Size(), mod: info.ModTime()}, true
}

// newDatabase makes an empty KDBX 4 vault with one root group, as KeePass apps expect.
func newDatabase(password string) *gokeepasslib.Database {
	db := gokeepasslib.NewDatabase(gokeepasslib.WithDatabaseKDBXVersion4())
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	if kdf := db.Header.FileHeaders.KdfParameters; kdf != nil {
		kdf.Memory, kdf.Iterations = kdfMemory, kdfIterations
	}
	root := gokeepasslib.NewGroup()
	root.Name = "Vault"
	db.Content.Root.Groups = []gokeepasslib.Group{root}
	db.Content.Meta.DatabaseName = "Vault"
	db.Content.Meta.Generator = "Polymux"
	return db
}

// readDatabase decrypts a vault and leaves its protected values unlocked in memory.
func readDatabase(path, password string) (db *gokeepasslib.Database, err error) {
	defer func() {
		if recover() != nil {
			db, err = nil, ErrKDBX
		}
	}()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !IsKDBX(data) {
		return nil, ErrKDBX
	}
	db = gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	if err := gokeepasslib.NewDecoder(bytes.NewReader(data)).Decode(db); err != nil {
		return nil, ErrKDBX
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, ErrKDBX
	}
	if db.Content == nil || db.Content.Root == nil || len(db.Content.Root.Groups) == 0 {
		return nil, ErrKDBX
	}
	return db, nil
}

// encodeDatabase serializes an unlocked database with fresh seeds and leaves it unlocked again.
// A failure can leave values locked, so callers reload the database from disk after one.
func encodeDatabase(db *gokeepasslib.Database) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			data, err = nil, errors.New("could not encode the vault")
		}
	}()
	headers := db.Header.FileHeaders
	refresh := func(b []byte) { _, _ = rand.Read(b) }
	refresh(headers.MasterSeed)
	refresh(headers.EncryptionIV)
	if db.Header.IsKdbx4() {
		if kdf := headers.KdfParameters; kdf != nil {
			refresh(kdf.Salt[:])
		}
		// The inner stream key can change only while values are unlocked.
		refresh(db.Content.InnerHeader.InnerRandomStreamKey)
	} else {
		refresh(headers.TransformSeed)
		refresh(headers.ProtectedStreamKey)
		refresh(headers.StreamStartBytes)
	}
	if err := db.LockProtectedEntries(); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := gokeepasslib.NewEncoder(&buffer).Encode(db); err != nil {
		return nil, err
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// writeAtomic replaces path in one rename, so a sync service never sees half a file.
func writeAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(data)
	if err := errors.Join(writeErr, file.Chmod(0600), file.Sync(), file.Close()); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
