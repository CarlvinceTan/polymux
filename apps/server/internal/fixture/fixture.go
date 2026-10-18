//go:build verifyfixture

// Package fixture seeds the verification data set for verifyfixture builds.
package fixture

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/agentruntime"
	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

//go:embed testdata/pixel.png
var pixel []byte

const (
	marker = ".verify-fixture"
	phrase = "polymux verify lighthouse"
)

// ErrRefused is returned when the data dir is real user data or Application Support.
var ErrRefused = errors.New("verify fixture refused")

var errDone = errors.New("fixture already seeded")

// Base is the fixed clock for every seeded timestamp.
var Base = time.Date(2026, 10, 12, 9, 0, 0, 0, time.FixedZone("CST", 8*3600))

var names = map[string]string{
	"t-chief": "Chief", "t-aria": "Aria", "t-milo": "Milo",
	"t-nova": "Nova", "t-approval": "Approval", "t-fern": "Fern",
}

// ThreadIDs are the twelve sessions a seeded server lists.
var ThreadIDs = []string{
	"t-chief", "t-aria", "t-milo", "t-nova", "t-long", "t-stream",
	"t-approval", "grp-trip", "t-media", "t-activity", "t-fern", "grp-empty",
}

func Register(fs *flag.FlagSet) *bool {
	return fs.Bool("verify-fixture", false, "seed an empty data dir with the verification fixture")
}

// Port maps a worktree path into 20000–29999.
func Port(root string) int {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return 20000 + int(binary.BigEndian.Uint32(sum[:4])%10000)
}

// Apply seeds dir (or <worktree>/.build/verify-data) and records the loopback address.
func Apply(dir string) (string, string, error) {
	root, err := worktree()
	if err != nil {
		return "", "", err
	}
	if dir == "" {
		dir = filepath.Join(root, ".build", "verify-data")
	}
	dir = expandHome(dir)
	if err := Seed(dir); err != nil {
		return "", "", err
	}
	port := Port(root)
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	body, err := json.Marshal(map[string]any{"addr": addr, "port": port, "dataDir": dir})
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Join(root, ".build"), 0755); err != nil {
		return "", "", err
	}
	path := filepath.Join(root, ".build", "verify-session.json")
	if err := os.WriteFile(path, append(body, '\n'), 0644); err != nil {
		return "", "", err
	}
	return dir, addr, nil
}

func worktree() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("verify fixture: no worktree")
		}
		dir = parent
	}
}

// Agents are the six direct-conversation profiles. Their thread ids match on main.
func Agents(model string) []agentruntime.Definition {
	out := make([]agentruntime.Definition, 0, len(names))
	for id, name := range names {
		out = append(out, agentruntime.Definition{
			Agent:  contract.Agent{ID: id, Name: name, Role: "Verify fixture", Model: model},
			Prompt: "Verification fixture.",
		})
	}
	return out
}

// HoldStream puts the streaming row back after startup recovery, and drops the recovery event
// it produced, so a restart serves the same fixture state as the first start.
func HoldStream(ctx context.Context, s *store.Store) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET status='streaming', data=json_set(data,'$.status','streaming') WHERE id='m-stream'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE thread_id='t-stream'`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET last_seq=(SELECT max(seq) FROM messages WHERE thread_id='t-stream') WHERE id='t-stream'`); err != nil {
		return err
	}
	return tx.Commit()
}

// Seed fills an empty dir, or does nothing when this fixture already owns it.
func Seed(dir string) error {
	dir = expandHome(dir)
	switch err := refuse(dir); {
	case errors.Is(err, errDone):
		return nil
	case err != nil:
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(dir, "polymux.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	if err := fill(ctx, db, dir); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"pinned": []string{"t-chief"}, "threads": ThreadIDs, "base": Base.Format(time.RFC3339)})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, marker), append(body, '\n'), 0644)
}

func refuse(dir string) error {
	abs, err := filepath.Abs(expandHome(dir))
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Clean(filepath.Join(home, "Library", "Application Support", "Polymux"))
	abs = filepath.Clean(abs)
	if strings.EqualFold(abs, root) || strings.HasPrefix(strings.ToLower(abs), strings.ToLower(root)+string(filepath.Separator)) {
		return fmt.Errorf("%w: path is under Application Support", ErrRefused)
	}
	entries, err := os.ReadDir(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	marked := false
	for _, entry := range entries {
		if entry.Name() == marker {
			marked = true
		}
	}
	if !marked {
		return fmt.Errorf("%w: data dir is not empty", ErrRefused)
	}
	if _, err := os.Stat(filepath.Join(abs, "polymux.db")); err == nil {
		return errDone
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func expandHome(dir string) string {
	if dir != "~" && !strings.HasPrefix(dir, "~/") {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return dir
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), "/"))
}

func fill(ctx context.Context, db *store.Store, dir string) error {
	media := filepath.Join(dir, "attachments", "pixel.png")
	if err := os.MkdirAll(filepath.Dir(media), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(media, pixel, 0600); err != nil {
		return err
	}
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, agent := range Agents("verify") {
		raw, err := json.Marshal(agent.Agent)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id,data) VALUES(?,?)`, agent.Agent.ID, string(raw)); err != nil {
			return err
		}
	}
	threads := []struct{ id, title, lead, members string }{
		{"t-chief", "", "t-chief", "t-chief"},
		{"t-aria", "", "t-aria", "t-aria"},
		{"t-milo", "", "t-milo", "t-milo"},
		{"t-nova", "", "t-nova", "t-nova"},
		{"t-approval", "", "t-approval", "t-approval"},
		{"t-fern", "", "t-fern", "t-fern"},
		{"t-long", "Long", "t-chief", "t-chief,t-aria"},
		{"t-stream", "Stream", "t-nova", "t-nova,t-fern"},
		{"grp-trip", "Trip", "t-chief", "t-chief,t-aria,t-milo"},
		{"t-media", "Media", "t-aria", "t-aria,t-milo"},
		{"t-activity", "Activity", "t-chief", "t-chief,t-milo"},
		{"grp-empty", "Empty", "t-nova", "t-nova,t-fern"},
	}
	for _, thread := range threads {
		if _, err := tx.ExecContext(ctx, `INSERT INTO threads(id,agent_id,title,lead_agent_id) VALUES(?,?,?,?)`, thread.id, thread.lead, thread.title, thread.lead); err != nil {
			return err
		}
		for position, member := range strings.Split(thread.members, ",") {
			if _, err := tx.ExecContext(ctx, `INSERT INTO thread_members(thread_id,agent_id,position) VALUES(?,?,?)`, thread.id, member, position); err != nil {
				return err
			}
		}
	}
	if err := insert(ctx, tx, msg("m-chief-user", "t-chief", "user", "done", "", "Hello", Base, "")); err != nil {
		return err
	}
	if err := insert(ctx, tx, msg("m-chief-assistant", "t-chief", "assistant", "done", "t-chief", "Ready when you are.", Base.Add(time.Second), "")); err != nil {
		return err
	}
	for _, item := range []struct {
		thread string
		n      int
	}{{"t-aria", 3}, {"t-milo", 1}, {"t-nova", 12}} {
		for i := 0; i < item.n; i++ {
			id := fmt.Sprintf("m-%s-%d", item.thread, i+1)
			text := fmt.Sprintf("unread %d", i+1)
			if err := insert(ctx, tx, msg(id, item.thread, "assistant", "done", item.thread, text, Base.Add(time.Duration(i)*time.Second), "")); err != nil {
				return err
			}
		}
	}
	question := store.FocusQuestion
	focus := contract.Message{
		ID: "focus:t-approval", ThreadID: "t-approval", Role: "assistant", Status: "done", CreatedAt: Base.Format(time.RFC3339),
		Parts: []contract.Part{{Kind: "choice", Question: &question, Options: store.FocusOptions}},
	}
	if err := insert(ctx, tx, focus); err != nil {
		return err
	}
	if err := insert(ctx, tx, msg("m-fern", "t-fern", "assistant", "done", "t-fern", phrase, Base, "")); err != nil {
		return err
	}
	for i := 0; i < 500; i++ {
		role, agent := "user", ""
		if i%2 == 1 {
			role, agent = "assistant", "t-chief"
		}
		id := fmt.Sprintf("m-long-%03d", i+1)
		if err := insert(ctx, tx, msg(id, "t-long", role, "done", agent, fmt.Sprintf("long %03d", i+1), Base.Add(time.Duration(i)*time.Second), "")); err != nil {
			return err
		}
	}
	// grp-empty is a group: a direct thread id is the agent id, so it cannot be the empty session.
	trips := []contract.Message{
		msg("m-trip-1", "grp-trip", "assistant", "done", "t-chief", "Let's plan the trip", Base, ""),
		msg("m-trip-2", "grp-trip", "assistant", "done", "t-aria", "I'll bring snacks", Base.Add(time.Minute), "m-trip-1"),
		msg("m-trip-3", "grp-trip", "assistant", "done", "t-milo", "On my way", Base.Add(8*time.Minute), "m-trip-2"),
	}
	for _, message := range trips {
		if err := insert(ctx, tx, message); err != nil {
			return err
		}
	}
	if err := insert(ctx, tx, msg("m-stream", "t-stream", "assistant", "streaming", "t-nova", "Still writing", Base, "")); err != nil {
		return err
	}
	att := contract.Attachment{ID: "att-pixel", Kind: "image", Name: "pixel.png", MimeType: "image/png", Size: int64(len(pixel)), URL: "/attachments/att-pixel/content", IndexingState: "ready"}
	raw, err := json.Marshal(att)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO attachments(id,data,path) VALUES(?,?,?)`, att.ID, string(raw), media); err != nil {
		return err
	}
	mediaMsg := msg("m-media", "t-media", "user", "done", "", "See the photo", Base, "")
	mediaMsg.Parts = append(mediaMsg.Parts, contract.Part{Kind: "attachment", Attachment: &att})
	if err := insert(ctx, tx, mediaMsg); err != nil {
		return err
	}
	if err := insert(ctx, tx, msg("m-activity", "t-activity", "assistant", "done", "t-chief", "Read the notes", Base, "")); err != nil {
		return err
	}
	done := Base.Add(time.Minute).Format(time.RFC3339)
	activity := contract.Run{
		ID: "run-activity", JobID: "run-activity", ConversationID: "t-activity", AgentID: "t-chief",
		Trigger: "message", Status: "completed", Activity: "read", Summary: "Read the notes",
		Tools:     []contract.ToolActivity{{ID: "tool-read", RunID: "run-activity", Name: "read", Status: "completed"}},
		CreatedAt: Base.Format(time.RFC3339), UpdatedAt: done, FinishedAt: &done,
	}
	if err := putRun(ctx, tx, activity); err != nil {
		return err
	}
	next := Base.Add(365 * 24 * time.Hour)
	nextAt := next.Format(time.RFC3339)
	routineID := "routine-digest"
	routine := contract.Routine{
		ID: routineID, AgentID: "t-chief", Trigger: "interval", Prompt: "Morning digest", Status: "active",
		IntervalSeconds: 86400, At: Base.Format(time.RFC3339), CreatedAt: Base.Format(time.RFC3339), NextAt: &nextAt,
	}
	raw, err = json.Marshal(routine)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO routines(id,agent_id,next_at,data) VALUES(?,?,?,?)`, routine.ID, routine.AgentID, next.Unix(), string(raw)); err != nil {
		return err
	}
	for i := 0; i < 5; i++ {
		status := "completed"
		var cause *contract.Error
		if i == 4 {
			status = "failed"
			cause = &contract.Error{Code: "failed", Message: "digest failed"}
		}
		when := Base.Add(time.Duration(i) * time.Hour)
		finished := when.Add(time.Minute).Format(time.RFC3339)
		id := fmt.Sprintf("run-digest-%d", i+1)
		if err := putRun(ctx, tx, contract.Run{
			ID: id, JobID: id, ConversationID: "t-chief", AgentID: "t-chief", Trigger: "routine", RoutineID: &routineID,
			Status: status, Summary: status, Error: cause, Activity: "routine",
			CreatedAt: when.Format(time.RFC3339), UpdatedAt: finished, FinishedAt: &finished,
		}); err != nil {
			return err
		}
	}
	reviews := contract.Proactivity{Enabled: true, IntervalSeconds: int64(store.ReviewInterval / time.Second), NextAt: &nextAt}
	raw, err = json.Marshal(reviews)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO proactivity(agent_id,next_at,data) VALUES(?,?,?)`, "t-chief", next.Unix(), string(raw)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET last_seq=(SELECT COALESCE(max(seq),0) FROM messages WHERE thread_id=threads.id)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE threads SET last_read_seq=last_seq WHERE id NOT IN ('t-aria','t-milo','t-nova')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages_fts(message_id,thread_id,body) VALUES(?,?,?)`, "m-fern", "t-fern", phrase); err != nil {
		return err
	}
	return tx.Commit()
}

func msg(id, thread, role, status, agent, text string, at time.Time, reply string) contract.Message {
	body := text
	message := contract.Message{
		ID: id, ThreadID: thread, Role: role, Status: status, CreatedAt: at.Format(time.RFC3339),
		Parts: []contract.Part{{Kind: "text", Text: &body}},
	}
	if agent != "" {
		message.AgentID = &agent
	}
	if reply != "" {
		message.ReplyToID = &reply
		message.Reply = &contract.ReplyContext{MessageID: reply, Role: "assistant", Text: text}
	}
	return message
}

func insert(ctx context.Context, tx *sql.Tx, message contract.Message) error {
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(seq),0)+1 FROM messages WHERE thread_id=?`, message.ThreadID).Scan(&message.Seq); err != nil {
		return err
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(id,thread_id,seq,role,status,data) VALUES(?,?,?,?,?,?)`, message.ID, message.ThreadID, message.Seq, message.Role, message.Status, string(raw))
	return err
}

func putRun(ctx context.Context, tx *sql.Tx, run contract.Run) error {
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO runs(id,job_id,thread_id,agent_id,status,data) VALUES(?,?,?,?,?,?)`, run.ID, run.JobID, run.ConversationID, run.AgentID, run.Status, string(raw))
	return err
}
