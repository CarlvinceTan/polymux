package hub

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// hiddenIDs lists the IDs linked WhatsApp accounts write from in newer chats. Only the device record holds them.
func (b *bridges) hiddenIDs(ctx context.Context) []string {
	b.mu.Lock()
	known := b.hidden
	b.mu.Unlock()
	if known != nil {
		return known
	}
	path := filepath.Join(b.folder("whatsapp"), "bridge.db")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String())
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT lid FROM whatsmeow_device WHERE lid <> ''")
	if err != nil {
		b.log.Warn("read whatsapp device", "error", err)
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var lid string
		if rows.Scan(&lid) == nil {
			out = append(out, hiddenGhost(lid))
		}
	}
	if len(out) == 0 {
		return nil
	}
	b.mu.Lock()
	b.hidden = out
	b.mu.Unlock()
	return out
}

// hiddenGhost turns a device record's 123:4@lid into the ID its bridge ghost carries.
func hiddenGhost(lid string) string {
	user, _, _ := strings.Cut(lid, "@")
	user, _, _ = strings.Cut(user, ":")
	return "lid-" + user
}
