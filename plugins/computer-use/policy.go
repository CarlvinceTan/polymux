package main

import (
	"encoding/json"
	"net/http"
)

type input struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

func (s *service) gate(w http.ResponseWriter, r *http.Request) {
	var call struct {
		Tool   string          `json:"tool"`
		Args   json.RawMessage `json:"args"`
		Answer string          `json:"answer"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&call) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out := map[string]any{"allow": true}
	if call.Tool == "computer_use__call" {
		var args input
		_ = json.Unmarshal(call.Args, &args)
		spec := s.driver.Spec()
		if reason := spec.check(args); reason != "" {
			out = map[string]any{"reason": reason}
		} else if spec.isAction(args.Tool) {
			switch call.Answer {
			case "computer_use_once":
			case "":
				detail, _ := json.Marshal(args.Args)
				out = map[string]any{"ask": map[string]any{
					"title":  "Allow Computer Use?",
					"detail": args.Tool + " " + string(detail),
					"options": []map[string]string{
						{"id": "computer_use_once", "label": "Allow once", "outcome": "allow"},
						{"id": "deny", "label": "Deny", "outcome": "deny"},
					},
				}}
			default:
				out = map[string]any{"reason": "Computer Use action declined."}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
