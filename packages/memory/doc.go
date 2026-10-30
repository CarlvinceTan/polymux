// Package memory stores revision-checked documents isolated by owner and scope.
// Hosts authorize access to those identifiers; the store owns its SQLite files
// and preserves the existing document format when reopened.
package memory
