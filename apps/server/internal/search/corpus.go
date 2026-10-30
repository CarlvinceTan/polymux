package search

import (
	"context"
	"strings"

	engine "github.com/CarlvinceTan/polymux/packages/search"
)

type corpus struct {
	index *Index
	scope searchScope
}

func (c corpus) Lexical(ctx context.Context, query string) ([]engine.Match, error) {
	rows, err := c.index.lexical(ctx, c.scope, engine.Query(query))
	if err != nil {
		return nil, err
	}
	for _, e := range c.scope.entities {
		if strings.Contains(strings.ToLower(e.body), strings.ToLower(query)) {
			rows = append([]ranked{{ID: e.result.ID}}, rows...)
		}
	}
	return matches(rows), nil
}

func (c corpus) Vector(ctx context.Context, vector []float32, model string) ([]engine.Match, error) {
	rows, err := c.index.vector(ctx, c.scope, vector, model)
	return matches(rows), err
}

func matches(rows []ranked) []engine.Match {
	out := make([]engine.Match, len(rows))
	for n, row := range rows {
		out[n] = engine.Match{ID: row.ID, Snippet: row.Snippet}
	}
	return out
}
