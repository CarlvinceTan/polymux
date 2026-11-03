package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/CarlvinceTan/polymux/plugins/hub/internal/contract"
)

// oauthRedirect is the loopback address the app listens on while someone signs in. It's fixed
// because Microsoft matches redirect addresses exactly, so it can be registered in advance.
const oauthRedirect = "http://127.0.0.1:47666/mail/callback"

const signInLifetime = 10 * time.Minute

type oauthProvider struct {
	name   string
	issuer string
	scopes []string
	params []oauth2.AuthCodeOption
	imap   string
	smtp   string
}

var oauthProviders = map[string]oauthProvider{
	// Google has no narrower scope that opens IMAP. Without offline and consent, a second
	// sign-in gets no refresh token.
	"google": {name: "Google", issuer: "https://accounts.google.com", scopes: []string{"openid", "email", "https://mail.google.com/"},
		params: []oauth2.AuthCodeOption{oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent")},
		imap:   "imap.gmail.com:993", smtp: "smtp.gmail.com:587"},
	// Microsoft refuses scopes from two resources in one request; the OpenID ones are the exception.
	"microsoft": {name: "Microsoft", issuer: "https://login.microsoftonline.com/common/v2.0",
		scopes: []string{"openid", "email", "offline_access", "https://outlook.office.com/IMAP.AccessAsUser.All", "https://outlook.office.com/SMTP.Send"},
		imap:   "outlook.office365.com:993", smtp: "smtp.office365.com:587"},
}

var providerOrder = []string{"google", "microsoft"}

type oauthClient struct {
	ID     string `json:"id"`
	Secret string `json:"secret,omitempty"`
}

// signIn is one sign-in the app has open. The verifier never leaves this process.
type signIn struct {
	provider string
	state    string
	verifier string
	config   *oauth2.Config
	began    time.Time
}

type accessToken struct {
	value   string
	expires time.Time
}

// providers reports what the app can offer; setup is what's missing before it can.
func (m *mailbox) providers() []contract.HubMailProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]contract.HubMailProvider, 0, len(providerOrder))
	for _, id := range providerOrder {
		value := contract.HubMailProvider{ID: id, Name: oauthProviders[id].name, Setup: []contract.HubField{}}
		if _, ready := m.clients[id]; !ready {
			value.Setup = []contract.HubField{{ID: "clientId", Name: "Client ID"}, {ID: "clientSecret", Name: "Client secret", Secret: true}}
		}
		out = append(out, value)
	}
	return out
}

func (m *mailbox) setClient(provider string, input contract.HubMailClient) error {
	if _, known := oauthProviders[provider]; !known {
		return errMissing
	}
	if strings.TrimSpace(input.ClientId) == "" {
		return publicError("Enter a client ID.")
	}
	sealed, err := m.seal.seal("client-"+provider, oauthClient{ID: strings.TrimSpace(input.ClientId), Secret: strings.TrimSpace(input.ClientSecret)})
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[provider] = sealed
	return m.save()
}

func (m *mailbox) oauthConfig(ctx context.Context, provider string) (*oauth2.Config, error) {
	m.mu.Lock()
	sealed, ready := m.clients[provider]
	m.mu.Unlock()
	if !ready {
		return nil, publicError("Set up this sign-in first.")
	}
	var client oauthClient
	if err := m.seal.open("client-"+provider, sealed, &client); err != nil {
		return nil, err
	}
	var endpoints struct {
		Authorize string `json:"authorization_endpoint"`
		Token     string `json:"token_endpoint"`
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.issuer(provider)+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, err
	}
	response, err := m.oauth.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(&endpoints); err != nil {
		return nil, err
	}
	return &oauth2.Config{ClientID: client.ID, ClientSecret: client.Secret, RedirectURL: oauthRedirect, Scopes: oauthProviders[provider].scopes,
		Endpoint: oauth2.Endpoint{AuthURL: endpoints.Authorize, TokenURL: endpoints.Token, AuthStyle: oauth2.AuthStyleInParams}}, nil
}

// beginSignIn makes the address the app opens. PKCE keeps a stolen code useless to anyone else.
func (m *mailbox) beginSignIn(ctx context.Context, provider string) (contract.HubMailSignIn, error) {
	shape, known := oauthProviders[provider]
	if !known {
		return contract.HubMailSignIn{}, errMissing
	}
	config, err := m.oauthConfig(ctx, provider)
	if err != nil {
		return contract.HubMailSignIn{}, err
	}
	pending := &signIn{provider: provider, state: randomHex(16), verifier: oauth2.GenerateVerifier(), config: config, began: time.Now()}
	id := randomHex(8)
	m.mu.Lock()
	for key, value := range m.signIns {
		if time.Since(value.began) > signInLifetime {
			delete(m.signIns, key)
		}
	}
	m.signIns[id] = pending
	m.mu.Unlock()
	options := append([]oauth2.AuthCodeOption{oauth2.S256ChallengeOption(pending.verifier)}, shape.params...)
	return contract.HubMailSignIn{ID: id, URL: config.AuthCodeURL(pending.state, options...)}, nil
}

// emailClaim reads the address out of an ID token. It came straight from the
// provider over TLS, so its signature adds nothing, but it has to be for this client.
func emailClaim(token, client string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Audience string `json:"aud"`
		Email    string `json:"email"`
	}
	if json.Unmarshal(data, &claims) != nil || claims.Audience != client {
		return ""
	}
	return claims.Email
}

// finishSignIn trades the code for tokens and adds the mailbox the provider says they belong to.
func (m *mailbox) finishSignIn(ctx context.Context, id string, input contract.HubMailCallback) (contract.HubMailAccount, error) {
	m.mu.Lock()
	pending := m.signIns[id]
	delete(m.signIns, id)
	m.mu.Unlock()
	if pending == nil || time.Since(pending.began) > signInLifetime {
		return contract.HubMailAccount{}, publicError("That sign-in expired.")
	}
	shape := oauthProviders[pending.provider]
	failed := publicError(shape.name + " sign-in failed.")
	if input.Error != "" {
		return contract.HubMailAccount{}, failed
	}
	if input.State != pending.state {
		return contract.HubMailAccount{}, publicError("The sign-in response didn't match.")
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, m.oauth)
	token, err := pending.config.Exchange(ctx, input.Code, oauth2.VerifierOption(pending.verifier))
	if err != nil {
		return contract.HubMailAccount{}, errors.Join(failed, err)
	}
	if token.RefreshToken == "" {
		return contract.HubMailAccount{}, publicError(shape.name + " gave no way to stay signed in.")
	}
	raw, _ := token.Extra("id_token").(string)
	email := emailClaim(raw, pending.config.ClientID)
	if _, err := mail.ParseAddress(email); err != nil {
		return contract.HubMailAccount{}, publicError(shape.name + " didn't say which address signed in.")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	item := mailAccount{ID: "mail-" + randomHex(8), Name: email, Email: email, Provider: pending.provider, IMAP: shape.imap, SMTP: shape.smtp,
		secret: credential{Refresh: token.RefreshToken}}
	// Signing in again replaces the mailbox's tokens and leaves everything else about it alone.
	for _, saved := range m.list {
		if strings.EqualFold(saved.Email, email) {
			item.ID, item.Name, item.Signatures, item.Default, item.FilesSent = saved.ID, saved.Name, saved.Signatures, saved.Default, saved.FilesSent
		}
	}
	m.access[item.ID] = accessToken{value: token.AccessToken, expires: token.Expiry}
	return item.wire(""), m.keep(item)
}

// token returns a live access token, keeping any new refresh token a provider rotates in.
func (m *mailbox) token(ctx context.Context, item mailAccount) (string, error) {
	m.mu.Lock()
	cached, ok := m.access[item.ID]
	m.mu.Unlock()
	if ok && time.Until(cached.expires) > time.Minute {
		return cached.value, nil
	}
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	config, err := m.oauthConfig(ctx, item.Provider)
	if err != nil {
		return "", err
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, m.oauth)
	renewed, err := config.TokenSource(ctx, &oauth2.Token{RefreshToken: item.secret.Refresh}).Token()
	if err != nil {
		return "", errors.Join(publicError("Sign in to this mailbox again."), err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.access[item.ID] = accessToken{value: renewed.AccessToken, expires: renewed.Expiry}
	if renewed.RefreshToken != "" && renewed.RefreshToken != item.secret.Refresh {
		item.secret.Refresh = renewed.RefreshToken
		if err := m.keep(item); err != nil {
			return "", err
		}
	}
	return renewed.AccessToken, nil
}

func xoauthLine(user, token string) []byte {
	return []byte(fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", user, token))
}

// xoauth signs in to IMAP. A server that refuses sends a challenge, which is answered with nothing.
type xoauth struct{ user, token string }

func (x xoauth) Start() (string, []byte, error) { return "XOAUTH2", xoauthLine(x.user, x.token), nil }
func (x xoauth) Next([]byte) ([]byte, error)    { return []byte{}, nil }

// smtpOAuth is the same exchange for SMTP. deliver has already insisted on TLS.
type smtpOAuth struct{ user, token string }

func (x smtpOAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", xoauthLine(x.user, x.token), nil
}

func (x smtpOAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return []byte{}, nil
	}
	return nil, nil
}

func (s *service) signInRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT /mail/oauth/{provider}/client", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubMailClient](w, r)
		if ok {
			s.reply(w, s.mail.setClient(r.PathValue("provider"), input))
		}
	})
	mux.HandleFunc("POST /mail/oauth/{provider}", func(w http.ResponseWriter, r *http.Request) {
		value, err := s.mail.beginSignIn(r.Context(), r.PathValue("provider"))
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, value)
	})
	mux.HandleFunc("POST /mail/oauth/{provider}/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[contract.HubMailCallback](w, r)
		if !ok {
			return
		}
		value, err := s.mail.finishSignIn(r.Context(), r.PathValue("id"), input)
		if err != nil {
			s.failed(w, err)
			return
		}
		write(w, 200, value)
	})
}
