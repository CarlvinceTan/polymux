package vault

// Keys provides the unattended data key; implementations must store it securely.
type Keys interface {
	Load() ([]byte, error)
	Save([]byte) error
	Protection() string
}
