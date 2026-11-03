package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/plugins"
)

// bindGate puts a run under the gate plugins of its chats, before it can call a tool.
func (s *Server) bindGate(ctx context.Context, state *turnState, dir string) error {
	chats, err := s.boundChats(ctx, state.run)
	if err != nil {
		return err
	}
	run := state.run
	state.chats = chats
	state.gate = func(ctx context.Context, tool string, args json.RawMessage) plugins.Decision {
		return s.plugins.Check(ctx, chats, plugins.Call{Agent: run.AgentID, Dir: dir, Tool: tool, Args: args})
	}
	return nil
}

// boundChats lists a run's chat and the chats of what started it, so delegating work doesn't escape their rules.
func (s *Server) boundChats(ctx context.Context, run contract.Run) ([]string, error) {
	chats := []string{}
	for {
		chats = append(chats, run.ConversationID)
		owner, err := s.Store.TaskOwner(ctx, run.ConversationID)
		if err != nil {
			return nil, err
		}
		if owner != "" {
			chats = append(chats, owner)
		}
		if run.ParentRunID == nil {
			return chats, nil
		}
		run, err = s.Store.Run(ctx, *run.ParentRunID)
		if errors.Is(err, sql.ErrNoRows) {
			return chats, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// noLoopback keeps a request off this machine, so a local token read from disk can't undo a gate's rules through the API.
func noLoopback() http.RoundTripper {
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if ip := net.ParseIP(host); err != nil || ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			return errors.New("this chat's rules keep requests off this machine")
		}
		return nil
	}}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy, transport.DialContext = nil, dialer.DialContext
	return transport
}
