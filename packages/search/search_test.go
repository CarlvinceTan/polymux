package search_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/CarlvinceTan/polymux/packages/search"
)

type source struct {
	lexical []search.Match
	vectors []search.Match
	lexErr  error
	vecErr  error
}

func (s source) Lexical(context.Context, string) ([]search.Match, error) {
	return s.lexical, s.lexErr
}

func (s source) Vector(context.Context, []float32, string) ([]search.Match, error) {
	return s.vectors, s.vecErr
}

type embedder struct{ err error }

func (e embedder) Embed(context.Context, search.Input) ([]float32, string, error) {
	return []float32{1, 0}, "test@2", e.err
}

func TestSearch(t *testing.T) {
	failed := errors.New("provider unavailable")
	for _, test := range []struct {
		name     string
		embedder search.Embedder
		source   source
		ids      []string
		hybrid   bool
		err      error
	}{
		{
			name: "hybrid fusion", embedder: embedder{}, hybrid: true,
			source: source{lexical: []search.Match{{ID: "a"}, {ID: "b", Snippet: "lexical snippet"}}, vectors: []search.Match{{ID: "b"}, {ID: "a"}}},
			ids:    []string{"a", "b"},
		},
		{
			name: "lexical only", source: source{lexical: []search.Match{{ID: "a"}}}, ids: []string{"a"},
		},
		{
			name: "embedding failure", embedder: embedder{err: failed},
			source: source{lexical: []search.Match{{ID: "a"}}}, ids: []string{"a"},
		},
		{
			name: "vector failure discards partial results", embedder: embedder{},
			source: source{lexical: []search.Match{{ID: "a"}}, vectors: []search.Match{{ID: "untrusted-partial"}}, vecErr: failed}, ids: []string{"a"},
		},
		{
			name: "lexical failure", embedder: embedder{}, source: source{lexErr: failed}, err: failed,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			hits, hybrid, err := (search.Engine{Embedder: test.embedder}).Search(t.Context(), "fruit", test.source)
			if !errors.Is(err, test.err) || hybrid != test.hybrid {
				t.Fatalf("hybrid=%v err=%v", hybrid, err)
			}
			var ids []string
			for _, hit := range hits {
				ids = append(ids, hit.ID)
			}
			if !reflect.DeepEqual(ids, test.ids) {
				t.Fatalf("ids=%v want=%v", ids, test.ids)
			}
			if test.hybrid {
				want := 1.0/61 + 1.0/62
				if math.Abs(hits[0].Score-want) > 1e-12 || hits[1].Snippet != "lexical snippet" {
					t.Fatalf("fusion lost ranks or snippets: %+v", hits)
				}
			}
		})
	}
}

func TestSearchLimit(t *testing.T) {
	rows := make([]search.Match, 100)
	for n := range rows {
		rows[n].ID = fmt.Sprintf("doc-%03d", n)
	}
	hits, _, err := (search.Engine{}).Search(t.Context(), "query", source{lexical: rows})
	if err != nil || len(hits) != 40 || hits[39].ID != "doc-039" {
		t.Fatalf("limit/order: hits=%v err=%v", hits, err)
	}
}

func TestSearchScope(t *testing.T) {
	scope := scopedSource{t: t}
	hits, hybrid, err := (search.Engine{Embedder: queryEmbedder{t: t}}).Search(t.Context(), "raw query", scope)
	if err != nil || !hybrid || len(hits) != 1 || hits[0].ID != "allowed" {
		t.Fatalf("scope: %v %v %v", hits, hybrid, err)
	}
}

type scopedSource struct{ t *testing.T }

func (s scopedSource) Lexical(_ context.Context, query string) ([]search.Match, error) {
	if query != "raw query" {
		s.t.Errorf("query was rewritten before reaching the source: %q", query)
	}
	return []search.Match{}, nil
}

func (s scopedSource) Vector(_ context.Context, vector []float32, model string) ([]search.Match, error) {
	if model != "test@2" || !reflect.DeepEqual(vector, []float32{1, 0}) {
		s.t.Errorf("model/vector changed: %s %v", model, vector)
	}
	return []search.Match{{ID: "allowed"}}, nil
}

type queryEmbedder struct{ t *testing.T }

func (e queryEmbedder) Embed(_ context.Context, input search.Input) ([]float32, string, error) {
	if !input.Query || input.Text != "raw query" {
		e.t.Errorf("not a retrieval query: %+v", input)
	}
	return []float32{1, 0}, "test@2", nil
}
