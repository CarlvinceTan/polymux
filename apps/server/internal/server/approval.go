package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
	"github.com/CarlvinceTan/polymux/apps/server/internal/store"
)

// approvalTimeout is how long a gate's question waits for a person before the call is declined.
const approvalTimeout = 10 * time.Minute

type approvalWait struct{ answer chan string }

func approvalID(run, call string) string { return "approval:" + run + ":" + call }

// askApproval carries a gate's question to the chat and waits for an answer. The
// question, its options and what an answer means belong to the plugin.
func (s *Server) askApproval(ctx context.Context, state *turnState, call string, ask plugins.Ask, tool string, args json.RawMessage) (string, string, error) {
	title := clip(strings.TrimSpace(ask.Title), 256)
	if title == "" {
		return "", "", errors.New("This chat's rules asked for an approval without a question.")
	}
	if len(ask.Options) == 0 || len(ask.Options) > 8 {
		return "", "", errors.New("This chat's rules asked for an approval with no answers.")
	}
	detail := clip(strings.TrimSpace(ask.Detail), 4096)
	options := make([]contract.ApprovalOption, 0, len(ask.Options))
	seen := map[string]bool{}
	for _, option := range ask.Options {
		id, label := strings.TrimSpace(option.ID), strings.TrimSpace(option.Label)
		outcome := contract.ApprovalOptionOutcomeAllow
		switch option.Outcome {
		case contract.ApprovalOptionOutcomeAllow:
		case contract.ApprovalOptionOutcomeDeny:
			outcome = contract.ApprovalOptionOutcomeDeny
		default:
			return "", "", errors.New("This chat's rules offered an approval answer without an outcome.")
		}
		if id == "" || label == "" || len(id) > 64 || len(label) > 128 || seen[id] {
			return "", "", errors.New("This chat's rules offered an invalid approval answer.")
		}
		seen[id] = true
		options = append(options, contract.ApprovalOption{ID: id, Label: label, Outcome: outcome})
	}
	id := approvalID(state.run.ID, call)
	part := contract.Part{Kind: contract.PartKindApproval, Approval: &contract.Approval{
		Title: title, Status: contract.ApprovalStatusPending, Options: options,
	}}
	if detail != "" {
		part.Approval.Detail = &detail
	}
	message := contract.Message{
		ID: id, ThreadID: state.run.ConversationID, Role: contract.MessageRoleAssistant,
		Status: contract.MessageStatusDone, CreatedAt: store.Now(), AgentID: &state.run.AgentID,
		RunID: &state.run.ID, Parts: []contract.Part{part},
	}
	stored, err := s.Store.PostMessage(ctx, message)
	if err != nil {
		return "", "", err
	}
	// A resumed run can find the card it already answered.
	if stored.Parts[0].Approval != nil && stored.Parts[0].Approval.Status != contract.ApprovalStatusPending {
		if stored.Parts[0].Approval.AnswerID != nil {
			return *stored.Parts[0].Approval.AnswerID, stored.ID, nil
		}
		return "", stored.ID, errors.New("This approval was already closed.")
	}
	if state.status != nil {
		_ = state.status("Waiting for your approval")
	}
	if err := s.Store.ApprovalNotice(ctx, stored); err != nil && s.config.Log != nil {
		s.config.Log.Error("approval notice", "error", err, "message", stored.ID)
	}
	waiter := &approvalWait{answer: make(chan string, 1)}
	s.approvals.Store(stored.ID, waiter)
	defer s.approvals.Delete(stored.ID)
	select {
	case answer := <-waiter.answer:
		return answer, stored.ID, nil
	case <-ctx.Done():
		s.settleApproval(context.WithoutCancel(ctx), state.run.ConversationID, stored.ID, contract.ApprovalStatusExpired, "")
		return "", stored.ID, errors.New("The run stopped before this approval was answered.")
	case <-time.After(approvalTimeout):
		s.settleApproval(context.WithoutCancel(ctx), state.run.ConversationID, stored.ID, contract.ApprovalStatusExpired, "")
		return "", stored.ID, errors.New("This approval wasn't answered in time, so the call was declined.")
	}
}

// settleApproval closes the card and shows the answer in every client.
func (s *Server) settleApproval(ctx context.Context, thread, id, status, answer string) {
	if _, err := s.Store.SettleApproval(ctx, thread, id, status, answer); err != nil && s.config.Log != nil {
		s.config.Log.Error("settle approval", "error", err, "message", id)
	}
}

func (s *Server) answerApproval(w http.ResponseWriter, r *http.Request) {
	thread, ok := s.thread(w, r)
	if !ok {
		return
	}
	var answer struct {
		OptionID string `json:"optionID"`
	}
	if !decode(w, r, &answer) {
		return
	}
	card, err := s.Store.ApprovalCard(r.Context(), thread.ID, r.PathValue("messageID"))
	if err != nil {
		fail(w, http.StatusConflict, "approval", "This approval is unavailable or has already been answered.")
		return
	}
	approval := card.Parts[0].Approval
	if approval.Status != contract.ApprovalStatusPending {
		s.respond(w, card, nil)
		return
	}
	found := false
	for _, option := range approval.Options {
		if option.ID == answer.OptionID {
			found = true
			break
		}
	}
	if !found {
		fail(w, http.StatusBadRequest, "approval", "Pick one of the offered answers.")
		return
	}
	value, waiting := s.approvals.Load(card.ID)
	if !waiting {
		fail(w, http.StatusConflict, "approval", "This approval is no longer waiting.")
		return
	}
	select {
	case value.(*approvalWait).answer <- answer.OptionID:
		s.respond(w, card, nil)
	default:
		fail(w, http.StatusConflict, "approval", "This approval has already been answered.")
	}
}

// clip shortens text without cutting a rune in half.
func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	runes := []rune(text)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
