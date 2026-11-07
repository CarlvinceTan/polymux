package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
)

var hidden = []byte("[redacted]")

// redactor streams a reply with filled values replaced, holding back just enough to catch
// a value split across reads.
type redactor struct {
	source  io.ReadCloser
	values  [][]byte
	keep    int
	pending []byte
	ready   []byte
	done    bool
}

func newRedactor(source io.ReadCloser, values []string) *redactor {
	r := &redactor{source: source}
	seen := map[string]bool{}
	for _, value := range values {
		escaped, _ := json.Marshal(value)
		for _, form := range []string{value, url.QueryEscape(value), string(escaped[1 : len(escaped)-1])} {
			if form == "" || seen[form] {
				continue
			}
			seen[form] = true
			r.values = append(r.values, []byte(form))
			r.keep = max(r.keep, len(form)-1)
		}
	}
	return r
}

func (r *redactor) Read(p []byte) (int, error) {
	for len(r.ready) == 0 {
		if r.done {
			return 0, io.EOF
		}
		chunk := make([]byte, 32<<10)
		n, err := r.source.Read(chunk)
		r.pending = append(r.pending, chunk[:n]...)
		for _, value := range r.values {
			r.pending = bytes.ReplaceAll(r.pending, value, hidden)
		}
		if err != nil {
			r.ready, r.pending, r.done = r.pending, nil, true
			if err != io.EOF {
				return 0, err
			}
			continue
		}
		if cut := len(r.pending) - r.keep; cut > 0 {
			r.ready = append(r.ready, r.pending[:cut]...)
			r.pending = append([]byte(nil), r.pending[cut:]...)
		}
	}
	n := copy(p, r.ready)
	r.ready = r.ready[n:]
	return n, nil
}

func (r *redactor) Close() error { return r.source.Close() }
