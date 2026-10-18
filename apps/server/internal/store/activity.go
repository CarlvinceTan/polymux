package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
)

var activityKinds = map[string][2]string{
	"agent.messaged":  {"Messaged", ""},
	"routine.created": {"Created", "clock"}, "routine.updated": {"Updated", "clock"},
	"routine.deleted": {"Deleted", "clock"}, "routine.paused": {"Paused", "clock"},
	"routine.resumed": {"Resumed", "clock"}, "routine.fired": {"Ran", "clock"},
	"memory.saved": {"Saved", "brain"}, "memory.deleted": {"Deleted", "brain"},
	"skill.saved": {"Saved", "sparkles"}, "skill.removed": {"Removed", "sparkles"},
	"subagent.started": {"Started", "arrow.triangle.branch"}, "subagent.finished": {"Finished", "arrow.triangle.branch"},
	"connector.added": {"Connected", "powerplug"}, "connector.removed": {"Disconnected", "powerplug"},
	"instructions.updated": {"Updated", "doc.text"},
	"device.added":         {"Connected", "desktopcomputer"}, "device.removed": {"Disconnected", "desktopcomputer"},
}

func NewActivity(kind, id, label string) (contract.Activity, error) {
	style, ok := activityKinds[kind]
	if !ok {
		return contract.Activity{}, errors.New("invalid activity kind")
	}
	return contract.Activity{Kind: kind, Verb: style[0], ObjectID: id,
		ObjectLabel: boundedText(strings.Join(strings.Fields(label), " "), 160), IconOrAgentID: style[1], At: Now()}, nil
}

func activityTx(ctx context.Context, tx *sql.Tx, thread, actor string, payload contract.Activity) error {
	_, err := appendTx(ctx, tx, contract.Event{ConversationID: thread, Type: "activity", AgentID: &actor, Payload: &payload, CreatedAt: payload.At}, nil)
	return err
}

func (s *Store) Activity(ctx context.Context, thread, actor string, payload contract.Activity) error {
	if _, err := NewActivity(payload.Kind, payload.ObjectID, payload.ObjectLabel); err != nil {
		return err
	}
	if payload.At == "" {
		payload.At = Now()
	}
	_, err := s.Append(ctx, contract.Event{ConversationID: thread, Type: "activity", AgentID: &actor, Payload: &payload, CreatedAt: payload.At}, nil)
	return err
}

func routineActivity(ctx context.Context, tx *sql.Tx, r contract.Routine, kind string) error {
	payload, err := NewActivity(kind, r.ID, r.Prompt)
	if err != nil {
		return err
	}
	return activityTx(ctx, tx, r.AgentID, r.AgentID, payload)
}

func childActivity(ctx context.Context, tx *sql.Tx, event contract.Event) error {
	run := event.Run
	kind := "subagent.started"
	if event.Type == "run.done" {
		kind = "subagent.finished"
	}
	label := run.AgentID
	if err := tx.QueryRowContext(ctx, `SELECT json_extract(data,'$.name') FROM agents WHERE id=?`, run.AgentID).Scan(&label); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	payload, err := NewActivity(kind, run.ID, label)
	if err != nil {
		return err
	}
	if err := activityTx(ctx, tx, run.ConversationID, run.AgentID, payload); err != nil {
		return err
	}
	var thread string
	err = tx.QueryRowContext(ctx, `SELECT thread_id FROM runs WHERE id=?`, *run.ParentRunID).Scan(&thread)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if thread != run.ConversationID {
		return activityTx(ctx, tx, thread, run.AgentID, payload)
	}
	return nil
}

// PeerHistory exposes only explicitly exchanged text, never either agent's private context.
func (s *Store) PeerHistory(ctx context.Context, first, second string, before int64) ([]contract.Message, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT rowid,data,input FROM deliveries
 WHERE ((source_agent=? AND target_agent=?) OR (source_agent=? AND target_agent=?))
 AND json_extract(data,'$.intent')!='handoff'
 AND (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT 50`, first, second, second, first, before, before)
	if err != nil {
		return nil, err
	}
	type exchange struct {
		seq      int64
		delivery contract.PeerDelivery
		work     Work
	}
	exchanges := []exchange{}
	for rows.Next() {
		var e exchange
		var data, input string
		if err := rows.Scan(&e.seq, &data, &input); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &e.delivery); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(input), &e.work); err != nil {
			rows.Close()
			return nil, err
		}
		exchanges = append(exchanges, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	messages := []contract.Message{}
	for _, e := range exchanges {
		// Group handoffs carry conversation context rather than a private peer message.
		if e.delivery.Intent == "handoff" {
			continue
		}
		var data string
		err := s.DB.QueryRowContext(ctx, `SELECT data FROM messages WHERE id=?`, "receipt:"+e.delivery.ID).Scan(&data)
		if err == nil {
			var reply contract.Message
			if err := json.Unmarshal([]byte(data), &reply); err != nil {
				return nil, err
			}
			reply.Seq = e.seq
			messages = append(messages, reply)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		request := e.work.Message
		request.ID = "peer:" + e.delivery.ID
		request.Role, request.AgentID, request.Status, request.Seq = "assistant", &e.delivery.SourceAgentID, "done", e.seq
		messages = append(messages, request)
	}
	return messages, nil
}

func routineTx(ctx context.Context, tx *sql.Tx, owner, id string) (contract.Routine, error) {
	var r contract.Routine
	var data string
	err := tx.QueryRowContext(ctx, `SELECT data FROM routines WHERE agent_id=? AND id=?`, owner, id).Scan(&data)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(data), &r)
	return r, err
}

type RoutinePatch struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Prompt  *string `json:"prompt,omitempty"`
}

func (s *Store) PatchRoutine(ctx context.Context, owner, id string, patch RoutinePatch) (contract.Routine, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return contract.Routine{}, err
	}
	defer tx.Rollback()
	r, err := routineTx(ctx, tx, owner, id)
	if err != nil {
		return r, err
	}
	if r.Status != "active" && r.Status != "paused" {
		return r, ErrSchedule
	}
	kinds := []string{}
	if patch.Prompt != nil && *patch.Prompt != r.Prompt {
		if strings.TrimSpace(*patch.Prompt) == "" || len(*patch.Prompt) > 4096 {
			return r, ErrSchedule
		}
		r.Prompt = *patch.Prompt
		kinds = append(kinds, "routine.updated")
	}
	if patch.Enabled != nil {
		status, kind := "paused", "routine.paused"
		if *patch.Enabled {
			status, kind = "active", "routine.resumed"
		}
		if r.Status != status {
			r.Status = status
			kinds = append(kinds, kind)
		}
	}
	var next any
	if r.Status == "active" {
		date := r.At
		if r.NextAt != nil {
			date = *r.NextAt
		}
		at, err := time.Parse(time.RFC3339, date)
		if err != nil {
			return r, err
		}
		if at.Before(time.Now()) {
			at = time.Now().Add(time.Second)
		}
		text := at.UTC().Format(time.RFC3339)
		r.NextAt, next = &text, at.Unix()
	} else {
		r.NextAt = nil
	}
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE routines SET data=?,next_at=? WHERE id=? AND agent_id=?`, string(data), next, id, owner); err != nil {
		return r, err
	}
	for _, kind := range kinds {
		if err := routineActivity(ctx, tx, r, kind); err != nil {
			return r, err
		}
	}
	return r, tx.Commit()
}

func (s *Store) ChildMessages(ctx context.Context, run string) ([]contract.Message, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM messages WHERE json_extract(data,'$.runID')=? ORDER BY seq LIMIT 1000`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := []contract.Message{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var message contract.Message
		if err := json.Unmarshal([]byte(data), &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
}
