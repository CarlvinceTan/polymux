package store

import (
	"context"
	"encoding/json"
)

type activityBot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type activityRun struct {
	ID             string `json:"id"`
	AgentID        string `json:"agentID"`
	ConversationID string `json:"conversationID"`
	ParentRunID    string `json:"parentRunID,omitempty"`
	Status         string `json:"status"`
	Activity       string `json:"activity"`
}

type activityDelivery struct {
	ID             string `json:"id"`
	SourceAgentID  string `json:"sourceAgentID"`
	TargetAgentID  string `json:"targetAgentID"`
	ConversationID string `json:"conversationID"`
	ParentRunID    string `json:"parentRunID"`
	Intent         string `json:"intent"`
	Status         string `json:"status"`
}

type PluginActivity struct {
	Bots       []activityBot      `json:"bots"`
	Runs       []activityRun      `json:"runs"`
	Deliveries []activityDelivery `json:"deliveries"`
}

// PluginActivity exposes only coordination metadata: no messages, run summaries, or tool data.
func (s *Store) PluginActivity(ctx context.Context) (PluginActivity, error) {
	var result PluginActivity
	var err error
	if result.Bots, err = activityRows[activityBot](ctx, s, `SELECT data FROM agents ORDER BY id`); err != nil {
		return result, err
	}
	if result.Runs, err = activityRows[activityRun](ctx, s, `SELECT data FROM runs ORDER BY rowid DESC LIMIT 120`); err != nil {
		return result, err
	}
	result.Deliveries, err = activityRows[activityDelivery](ctx, s, `SELECT data FROM deliveries ORDER BY rowid DESC LIMIT 120`)
	return result, err
}

func activityRows[T any](ctx context.Context, s *Store, query string) ([]T, error) {
	rows, err := s.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []T{}
	for rows.Next() {
		var raw []byte
		var item T
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
