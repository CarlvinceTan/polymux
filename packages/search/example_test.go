package search_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/CarlvinceTan/polymux/packages/search"
)

type documents map[string]string

func (d documents) Lexical(_ context.Context, query string) ([]search.Match, error) {
	matches := []search.Match{}
	for id, text := range d {
		if strings.Contains(text, query) {
			matches = append(matches, search.Match{ID: id, Snippet: text})
		}
	}
	return matches, nil
}

func (documents) Vector(context.Context, []float32, string) ([]search.Match, error) {
	return []search.Match{}, nil
}

func ExampleEngine_Search() {
	engine := search.Engine{}
	corpus := documents{"note-1": "orange balloon", "note-2": "blue bicycle"}
	hits, semantic, err := engine.Search(context.Background(), "balloon", corpus)
	if err != nil {
		panic(err)
	}
	fmt.Println(hits[0].ID, hits[0].Snippet, semantic)
	// Output: note-1 orange balloon false
}
