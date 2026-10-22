package search

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type searchScope struct {
	thread   string
	kind     string
	entities []entity
}

func (s searchScope) predicate() string {
	predicate := `(?='' OR m.thread_id=?)`
	switch s.kind {
	case "bots", "conversations":
		return predicate + ` AND 0`
	case "media", "files":
		media := `substr(json_extract(p.value,'$.attachment.mimeType'),1,6) IN ('image/','video/','audio/')`
		if s.kind == "files" {
			media = `NOT (` + media + `)`
		}
		predicate += ` AND EXISTS(SELECT 1 FROM json_each(m.data,'$.parts') p
			WHERE json_type(p.value,'$.attachment')='object' AND ` + media + `)`
	}
	return predicate
}

type entity struct {
	result contract.SearchResult
	body   string
}

func (i *Index) entities(ctx context.Context) ([]entity, error) {
	agents, err := i.Store.Agents(ctx)
	if err != nil {
		return nil, err
	}
	threads, err := i.Store.Threads(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]contract.Agent{}
	for _, agent := range agents {
		byID[agent.ID] = agent
	}
	out := []entity{}
	for _, thread := range threads {
		names := []string{}
		for _, id := range thread.AgentIDs {
			names = append(names, byID[id].Name)
		}
		snippet := strings.Join(names, ", ")
		out = append(out, entity{
			result: contract.SearchResult{
				ID: "conversation/" + thread.ID, Kind: "conversations", Title: thread.Title,
				Snippet: snippet, ThreadID: thread.ID, AgentID: &thread.AgentID,
			},
			body: thread.Title + "\n" + snippet,
		})
		if thread.ID != thread.AgentID {
			continue
		}
		agent := byID[thread.AgentID]
		out = append(out, entity{
			result: contract.SearchResult{
				ID: "bot/" + agent.ID, Kind: "bots", Title: agent.Name,
				Snippet: agent.Role, ThreadID: thread.ID, AgentID: &agent.ID,
			},
			body: agent.Name + "\n" + agent.Role,
		})
	}
	return out, nil
}

func (i *Index) indexEntities(ctx context.Context) error {
	if i.Embedder == nil {
		return nil
	}
	entities, err := i.entities(ctx)
	if err != nil {
		return err
	}
	for _, e := range entities {
		var body string
		err := i.Store.DB.QueryRowContext(ctx, `SELECT body FROM search_entities WHERE id=?`, e.result.ID).Scan(&body)
		if err == nil && body == e.body {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		vector, model, err := i.Embedder.Embed(ctx, Input{Text: e.body})
		if err != nil {
			return err
		}
		blob, err := vec.SerializeFloat32(vector)
		if err != nil {
			return err
		}
		// A deleted conversation must not be recreated by an in-flight embedding.
		_, err = i.Store.DB.ExecContext(ctx, `INSERT INTO search_entities(id,thread_id,body,embedding,model)
			SELECT ?,id,?,?,? FROM threads WHERE id=?
			ON CONFLICT(id) DO UPDATE SET body=excluded.body,embedding=excluded.embedding,model=excluded.model`,
			e.result.ID, e.body, blob, model, e.result.ThreadID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *Index) entityRanks(ctx context.Context, entities []entity, model string, blob []byte) ([]ranked, error) {
	live := map[string]string{}
	for _, e := range entities {
		live[e.result.ID] = e.body
	}
	rows, err := i.Store.DB.QueryContext(ctx, `SELECT id,body,vec_distance_cosine(embedding,?)
		FROM search_entities WHERE model=? ORDER BY vec_distance_cosine(embedding,?)`, blob, model, blob)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ranked{}
	for rows.Next() {
		var r ranked
		var body string
		if err := rows.Scan(&r.ID, &body, &r.distance); err != nil {
			return nil, err
		}
		if current, ok := live[r.ID]; ok && current == body {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

func media(mime string) bool {
	return strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/") || strings.HasPrefix(mime, "audio/")
}

func (i *Index) Global(ctx context.Context, query, kind string) ([]contract.SearchResult, bool, error) {
	entities, err := i.entities(ctx)
	if err != nil {
		return nil, false, err
	}
	scope := searchScope{kind: kind, entities: []entity{}}
	metadata := map[string]contract.SearchResult{}
	titles := map[string]string{}
	botThreads := map[string]bool{}
	for _, e := range entities {
		if e.result.Kind == "bots" {
			botThreads[e.result.ThreadID] = true
		}
	}
	for _, e := range entities {
		if e.result.Kind == "conversations" {
			titles[e.result.ThreadID] = e.result.Title
		}
		if kind == "all" && e.result.Kind == "conversations" && botThreads[e.result.ThreadID] {
			continue
		}
		if kind == "all" || kind == e.result.Kind {
			scope.entities = append(scope.entities, e)
			metadata[e.result.ID] = e.result
		}
	}
	out := []contract.SearchResult{}
	if query == "" {
		for _, e := range scope.entities {
			out = append(out, e.result)
		}
		return out, false, nil
	}
	scores, snippets, semantic, err := i.ranks(ctx, scope, query)
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	for _, id := range rankedIDs(scores) {
		if result, ok := metadata[id]; ok {
			result.Score = scores[id]
			out = append(out, result)
			continue
		}
		hits, _, err := i.hits(ctx, map[string]float64{id: scores[id]}, snippets, semantic)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, false, err
		}
		hit := hits[0]
		m := hit.Message
		result := contract.SearchResult{
			ID: "message/" + m.ID, Kind: "messages", Title: titles[m.ThreadID],
			Snippet: hit.Snippet, ThreadID: m.ThreadID, MessageID: &m.ID, Score: hit.Score, Message: &m,
		}
		if kind == "all" || kind == "messages" {
			out = append(out, result)
		}
		for _, part := range m.Parts {
			a := part.Attachment
			if a == nil || seen[a.ID] || kind == "messages" {
				continue
			}
			category := "files"
			if media(a.MimeType) {
				category = "media"
			}
			if kind != "all" && kind != category {
				continue
			}
			seen[a.ID] = true
			file := result
			file.ID, file.Kind, file.Title, file.Attachment = "attachment/"+a.ID, category, a.Name, a
			out = append(out, file)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Score > out[b].Score })
	if len(out) > 40 {
		out = out[:40]
	}
	return out, semantic, nil
}
