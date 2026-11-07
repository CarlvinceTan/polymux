package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CarlvinceTan/polymux/apps/server/internal/contract"
	"github.com/CarlvinceTan/polymux/apps/server/internal/providers"
	"github.com/MichaelKinsy/PiG/ai"
)

// login is a provider sign-in running on the server; the app polls it and answers its prompts.
type login struct {
	mu       sync.Mutex
	state    contract.ProviderLogin
	answers  map[int]chan string
	changed  chan struct{}
	cancel   context.CancelFunc
	target   string
	existing bool
}

// update applies a change and wakes anyone waiting for the next revision.
func (l *login) update(change func(*contract.ProviderLogin)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	change(&l.state)
	l.state.Revision++
	close(l.changed)
	l.changed = make(chan struct{})
}

// snapshot returns the state once it is past revision after, or as it stands when ctx ends.
func (l *login) snapshot(ctx context.Context, after int) contract.ProviderLogin {
	for {
		l.mu.Lock()
		state, changed := l.state, l.changed
		state.Events = append([]contract.LoginEvent{}, state.Events...)
		l.mu.Unlock()
		if state.Revision > after || state.Done {
			return state
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return state
		}
	}
}

// ask shows a prompt and waits for its answer.
func (l *login) ask(ctx context.Context, prompt ai.AuthPrompt) (string, error) {
	answer := make(chan string, 1)
	var id int
	l.update(func(state *contract.ProviderLogin) {
		id = state.Revision + 1
		shown := &contract.LoginPrompt{ID: id, Type: prompt.Type()}
		switch p := prompt.(type) {
		case ai.AuthTextPrompt:
			shown.Message, shown.Placeholder = p.Message, p.Placeholder
		case ai.AuthSecretPrompt:
			shown.Message, shown.Placeholder = p.Message, p.Placeholder
		case ai.AuthManualCodePrompt:
			shown.Message, shown.Placeholder = p.Message, p.Placeholder
		case ai.AuthSelectPrompt:
			shown.Message = p.Message
			for _, option := range p.Options {
				shown.Options = append(shown.Options, contract.LoginChoice{ID: option.ID, Label: option.Label, Description: option.Description})
			}
		}
		state.Prompt = shown
		l.answers[id] = answer
	})
	defer l.update(func(state *contract.ProviderLogin) {
		delete(l.answers, id)
		if state.Prompt != nil && state.Prompt.ID == id {
			state.Prompt = nil
		}
	})
	select {
	case value := <-answer:
		return value, nil
	case <-ctx.Done():
		return "", context.Cause(ctx)
	}
}

func (l *login) notify(event ai.AuthEvent) {
	shown := contract.LoginEvent{Type: event.Type()}
	switch e := event.(type) {
	case ai.AuthInfoEvent:
		shown.Message = e.Message
		if len(e.Links) > 0 {
			shown.URL = e.Links[0].URL
		}
	case ai.AuthURLEvent:
		shown.URL, shown.Instructions = e.URL, e.Instructions
	case ai.AuthDeviceCodeEvent:
		shown.UserCode, shown.VerificationUri = e.UserCode, e.VerificationURI
	case ai.AuthProgressEvent:
		shown.Message = e.Message
	}
	l.update(func(state *contract.ProviderLogin) { state.Events = append(state.Events, shown) })
}

const (
	loginTimeout = 10 * time.Minute
	// loginKept is how long a finished sign-in stays readable.
	loginKept = 2 * time.Minute
	maxLogins = 4
)

func (s *Server) loginRoutes(reg *providers.Registry, mu *sync.Mutex) {
	var lock sync.Mutex
	logins := map[string]*login{}
	deviceID := sync.OnceValues(func() (string, error) { return loginDevice(s.config.DataDir) })
	find := func(w http.ResponseWriter, r *http.Request) *login {
		lock.Lock()
		defer lock.Unlock()
		l := logins[r.PathValue("login")]
		if l == nil {
			fail(w, 404, "unknown_login", "This sign-in has ended. Start again.")
		}
		return l
	}

	// finish adds or updates the Settings provider once its credential is stored.
	finish := func(ctx context.Context, l *login, kind string, credential ai.Credential) (*contract.Provider, error) {
		if kind == "groq" && credential.Type == ai.CredentialAPIKey {
			if err := providers.VerifyKey(ctx, providers.Builtin(kind).BaseURL, credential.Key); err != nil {
				return nil, err
			}
		}
		mu.Lock()
		defer mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, builtin := l.target, providers.Builtin(kind)
		list := reg.Settings.Providers()
		index := -1
		for i, p := range list {
			if p.ID == id {
				index = i
			}
		}
		if index < 0 && l.existing {
			return nil, errors.New("provider removed during sign-in")
		}
		if index < 0 {
			name := builtin.Name
			inherited := []providers.Model{}
			enabled := true
			for _, p := range list {
				if p.Type == kind {
					inherited, enabled = p.Models, p.Enabled
					break
				}
			}
			if credential.Type == ai.CredentialAPIKey {
				number := 1
				for _, p := range list {
					if p.Type == kind {
						number++
					}
				}
				name = fmt.Sprintf("API key %d", number)
				for slices.ContainsFunc(list, func(p providers.Provider) bool { return p.Type == kind && p.Name == name }) {
					number++
					name = fmt.Sprintf("API key %d", number)
				}
			} else {
				for suffix := 2; slices.ContainsFunc(list, func(p providers.Provider) bool { return p.Name == name }); suffix++ {
					name = fmt.Sprintf("%s %d", builtin.Name, suffix)
				}
			}
			list = append(list, providers.Provider{
				ID: id, Type: kind, Name: name, BaseURL: builtin.BaseURL,
				Enabled: enabled, Order: len(list), Models: inherited,
			})
			index = len(list) - 1
		}
		if list[index].Type != kind {
			return nil, errors.New("provider changed during sign-in")
		}
		store := reg.Credentials()
		var previous *ai.Credential
		if _, err := store.Modify(ctx, providers.Prefix+id, func(current *ai.Credential) (*ai.Credential, error) {
			if credential.Type == ai.CredentialAPIKey && current != nil && current.Type == ai.CredentialAPIKey && current.Key != "" {
				return nil, providers.ErrKeyExists
			}
			previous = current
			return &credential, nil
		}); err != nil {
			return nil, err
		}
		list[index].Models = providers.Models(builtin, &credential, list[index].Models)
		if err := reg.Save(list); err != nil {
			var restoreErr error
			if previous == nil {
				restoreErr = store.Delete(context.WithoutCancel(ctx), providers.Prefix+id)
			} else {
				_, restoreErr = store.Modify(context.WithoutCancel(ctx), providers.Prefix+id, func(*ai.Credential) (*ai.Credential, error) {
					return previous, nil
				})
			}
			return nil, errors.Join(err, restoreErr)
		}
		s.syncProviders(reg)
		saved := list[index]
		saved.Status = string(credential.Type)
		if credential.Type == ai.CredentialAPIKey && credential.Key != "" {
			saved.KeyHint = providers.KeyHint(credential.Key)
		}
		return &saved, nil
	}

	s.mux.HandleFunc("GET /providers/options", func(w http.ResponseWriter, r *http.Request) {
		if !nativeRequest(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		write(w, 200, providers.Options())
	})

	s.mux.HandleFunc("POST /providers/logins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !nativeRequest(w, r) {
			return
		}
		var body struct {
			Type     string `json:"type"`
			Auth     string `json:"auth"`
			Provider string `json:"provider"`
		}
		if !decode(w, r, &body) {
			return
		}
		id, kind := body.Provider, body.Type
		if id != "" {
			// Signing in again keeps the provider and its model choices.
			kind = ""
			for _, p := range reg.Settings.Providers() {
				if p.ID == id {
					kind = p.Type
				}
			}
		}
		builtin := providers.Builtin(kind)
		auth := ai.CredentialType(body.Auth)
		supported := slices.ContainsFunc(providers.Options(), func(option contract.ProviderOption) bool {
			return option.Type == kind && option.Auth == body.Auth
		})
		if builtin == nil || !supported {
			fail(w, 400, "invalid_login", "This provider can’t be signed in to.")
			return
		}
		if id != "" {
			credential, _ := reg.Credentials().Read(r.Context(), providers.Prefix+id)
			if credential != nil && credential.Type == ai.CredentialAPIKey && credential.Key != "" {
				fail(w, 409, "key_exists", "Remove the saved key before adding another.")
				return
			}
		}
		var device string
		if auth == ai.CredentialOAuth {
			var err error
			device, err = deviceID()
			if err != nil {
				fail(w, 500, "login_device", "Couldn’t prepare sign-in on this server.")
				return
			}
		}
		if id == "" {
			suffix := make([]byte, 16)
			_, _ = rand.Read(suffix)
			id = kind + "-" + hex.EncodeToString(suffix)
		}
		token := make([]byte, 16)
		_, _ = rand.Read(token)
		parent := s.ctx
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, loginTimeout)
		l := &login{state: contract.ProviderLogin{ID: hex.EncodeToString(token), Events: []contract.LoginEvent{}},
			answers: map[int]chan string{}, changed: make(chan struct{}), cancel: cancel, target: id, existing: body.Provider != ""}
		lock.Lock()
		active := 0
		for _, pending := range logins {
			pending.mu.Lock()
			done := pending.state.Done
			pending.mu.Unlock()
			if done {
				continue
			}
			active++
			if pending.target == id {
				lock.Unlock()
				cancel()
				fail(w, 409, "login_pending", "This provider already has a sign-in in progress.")
				return
			}
		}
		if active >= maxLogins {
			lock.Unlock()
			cancel()
			fail(w, 429, "too_many_logins", "Finish or cancel another sign-in first.")
			return
		}
		logins[l.state.ID] = l
		lock.Unlock()

		s.admission.fork()
		go func() {
			defer s.admission.done()
			defer cancel()
			// Only publish credentials after login completes and the provider still exists.
			models := ai.CreateModels(ai.CreateModelsOptions{Credentials: ai.NewInMemoryCredentialStore(), AuthContext: &providers.NoAmbient})
			defer models.Close()
			provider := &ai.ModelsProvider{ID: providers.Prefix + id, Name: builtin.Name, Auth: builtin.Auth}
			interaction := ai.AuthInteraction{Prompt: l.ask, Notify: l.notify}
			credential, err := models.LoginProvider(ctx, provider, auth, interaction, ai.LoginOptions{AgentName: new("Polymux"), GetDeviceID: func() string { return device }})
			var saved *contract.Provider
			if err == nil {
				saved, err = finish(ctx, l, kind, credential)
			}
			l.update(func(state *contract.ProviderLogin) {
				state.Done, state.Prompt, state.Provider = true, nil, saved
				switch {
				case err == nil:
				case errors.Is(err, context.Canceled):
					state.Error = "Sign-in cancelled."
				case errors.Is(err, context.DeadlineExceeded):
					state.Error = "Sign-in timed out. Start again."
				case errors.Is(err, providers.ErrInvalidKey):
					state.Error = "This API key was rejected. Check it and start sign-in again."
				default:
					state.Error = "Couldn’t connect this provider. Start sign-in again."
				}
			})
			time.AfterFunc(loginKept, func() {
				lock.Lock()
				delete(logins, l.state.ID)
				lock.Unlock()
			})
		}()
		write(w, 200, l.snapshot(r.Context(), -1))
	})

	s.mux.HandleFunc("GET /providers/logins/{login}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !nativeRequest(w, r) {
			return
		}
		l := find(w, r)
		if l == nil {
			return
		}
		after, err := strconv.Atoi(r.URL.Query().Get("after"))
		if err != nil {
			after = -1
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		write(w, 200, l.snapshot(ctx, after))
	})

	s.mux.HandleFunc("POST /providers/logins/{login}/answer", func(w http.ResponseWriter, r *http.Request) {
		if !nativeRequest(w, r) {
			return
		}
		l := find(w, r)
		if l == nil {
			return
		}
		var body struct {
			Prompt int    `json:"prompt"`
			Value  string `json:"value"`
		}
		if !decode(w, r, &body) {
			return
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		answer := l.answers[body.Prompt]
		if answer == nil || l.state.Prompt == nil || l.state.Prompt.ID != body.Prompt {
			fail(w, 409, "stale_prompt", "This step has already moved on.")
			return
		}
		prompt := l.state.Prompt
		if prompt.Type == "secret" && strings.TrimSpace(body.Value) == "" {
			fail(w, 400, "invalid_key", "Enter an API key or token.")
			return
		}
		if prompt.Type == "select" && !slices.ContainsFunc(prompt.Options, func(option contract.LoginChoice) bool {
			return option.ID == body.Value
		}) {
			fail(w, 400, "invalid_choice", "Choose one of the listed options.")
			return
		}
		delete(l.answers, body.Prompt)
		l.state.Prompt = nil
		l.state.Revision++
		close(l.changed)
		l.changed = make(chan struct{})
		answer <- body.Value
		w.WriteHeader(204)
	})

	s.mux.HandleFunc("DELETE /providers/logins/{login}", func(w http.ResponseWriter, r *http.Request) {
		if !nativeRequest(w, r) {
			return
		}
		if l := find(w, r); l != nil {
			l.cancel()
			w.WriteHeader(204)
		}
	})
}

func loginDevice(dir string) (string, error) {
	path := filepath.Join(dir, "provider-device")
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read sign-in device: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create sign-in directory: %w", err)
	}
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("create sign-in device: %w", err)
	}
	data[6], data[8] = data[6]&0x0f|0x40, data[8]&0x3f|0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", data[:4], data[4:6], data[6:8], data[8:10], data[10:])
	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		return "", fmt.Errorf("save sign-in device: %w", err)
	}
	return id, nil
}
