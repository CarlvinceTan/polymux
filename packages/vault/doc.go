// Package vault owns encrypted SQLite vault records, imports and credential-use operations.
// Callers supply a database, secure host-key storage and device authorization.
// Plaintext methods are for trusted adapters; editors expose only blank replacement fields.
package vault
