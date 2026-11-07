package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Judge rules on what a reply says when a pattern can't: whether "Finn's $255.60 covers it easily" means the user can
// afford it. It runs on its own pinned model, sees only the user's question and what they were shown, and is asked
// only after a check's pattern has failed, so it can rescue a correct answer but never fail one.
type Judge struct{ Upstream, Key, Model string }

// Ruling is one decision, kept with the run for review.
type Ruling struct {
	Question string `json:"question"`
	Verdict  bool   `json:"verdict"`
	Error    string `json:"error,omitempty"`
}

const judgeSystem = "You grade an assistant's replies for a benchmark. Read the replies the user saw and answer the question about them. " +
	"Answer YES only if the replies clearly and correctly do what the question asks; a hedge, a partial answer or a wrong figure is NO. Reply with YES or NO only."

// Ask rules on question for the replies in text.
func (j *Judge) Ask(question, text string) (bool, error) {
	body, _ := json.Marshal(map[string]any{"model": j.Model, "temperature": 0, "reasoning_effort": "low", "messages": []map[string]string{
		{"role": "system", "content": judgeSystem},
		{"role": "user", "content": "Question: " + question + "\n\nReplies the user saw:\n" + text},
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(j.Upstream, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+j.Key)
	request.Header.Set("User-Agent", "polymux-bench/0.1")
	request.Header.Set("x-opencode-session", "bench-judge")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil || len(reply.Choices) == 0 {
		return false, fmt.Errorf("judge reply: HTTP %d %v", response.StatusCode, err)
	}
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(reply.Choices[0].Message.Content)), "YES"), nil
}
