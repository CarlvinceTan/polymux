// Package sshagent serves a bot's saved SSH keys to its commands over a private unix
// socket. A command can sign with a key but never read it, so keys stay black boxes.
package sshagent

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var ownerName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Keys returns the keys saved for owner. It runs per connection, so a newly saved key
// works on the next command.
type Keys func(owner string) []agent.AddedKey

type socket struct {
	path     string
	listener net.Listener
}

type Agent struct {
	dir  string
	keys Keys
	// parent is the host agent a command would otherwise reach, kept alongside the bot's keys.
	parent string
	mu     sync.Mutex
	live   map[string]socket
	next   int
	closed bool
}

// Start prepares a private directory for agent sockets. Sockets are created per bot on
// first use and removed when the agent closes.
func Start(keys Keys) (*Agent, error) {
	// A unix socket path is short, so the sockets live in a temporary directory rather
	// than under the server's data directory.
	dir, err := os.MkdirTemp("", "polymux-ssh-")
	if err != nil {
		return nil, err
	}
	if len(dir) > 80 {
		_ = os.RemoveAll(dir)
		if dir, err = os.MkdirTemp("/tmp", "polymux-ssh-"); err != nil {
			return nil, err
		}
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, errors.Join(err, os.RemoveAll(dir))
	}
	return &Agent{dir: dir, keys: keys, parent: os.Getenv("SSH_AUTH_SOCK"), live: map[string]socket{}}, nil
}

// Env gives a command the environment that reaches its bot's keys. A bot with no keys
// keeps the environment it would otherwise have, so nothing is taken away.
func (a *Agent) Env(owner string) map[string]string {
	if len(a.keys(owner)) == 0 {
		return nil
	}
	path, err := a.listen(owner)
	if err != nil {
		return nil
	}
	return map[string]string{"SSH_AUTH_SOCK": path}
}

func (a *Agent) listen(owner string) (string, error) {
	if !ownerName.MatchString(owner) {
		return "", errors.New("invalid owner")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return "", errors.New("agent is closed")
	}
	if live, ok := a.live[owner]; ok {
		return live.path, nil
	}
	path := filepath.Join(a.dir, fmt.Sprintf("%d.sock", a.next))
	a.next++
	listener, err := net.Listen("unix", path)
	if err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return "", errors.Join(err, listener.Close())
	}
	a.live[owner] = socket{path: path, listener: listener}
	go a.serve(owner, path, listener)
	return path, nil
}

func (a *Agent) serve(owner, path string, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			identities, done := a.identities(owner, path)
			defer done()
			_ = agent.ServeAgent(identities, conn)
		}()
	}
}

// identities offers the bot's own keys first, then any the host agent already had.
func (a *Agent) identities(owner, own string) (agent.ExtendedAgent, func()) {
	keys := keyring(a.keys(owner))
	if a.parent == "" || a.parent == own {
		return &chained{own: keys}, func() {}
	}
	conn, err := net.Dial("unix", a.parent)
	if err != nil {
		return &chained{own: keys}, func() {}
	}
	return &chained{own: keys, parent: agent.NewClient(conn)}, func() { conn.Close() }
}

// chained adds a host agent's identities to the bot's own.
type chained struct {
	own    agent.ExtendedAgent
	parent agent.Agent
}

func (c *chained) List() ([]*agent.Key, error) {
	keys, err := c.own.List()
	if c.parent != nil {
		if extra, extraErr := c.parent.List(); extraErr == nil {
			keys = append(keys, extra...)
		}
	}
	return keys, err
}

func (c *chained) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	if signature, err := c.own.Sign(key, data); err == nil {
		return signature, nil
	}
	if c.parent == nil {
		return nil, errNoHostAgent
	}
	return c.parent.Sign(key, data)
}

func (c *chained) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	if signature, err := c.own.SignWithFlags(key, data, flags); err == nil {
		return signature, nil
	}
	if c.parent == nil {
		return nil, errNoHostAgent
	}
	if signer, ok := c.parent.(agent.ExtendedAgent); ok {
		return signer.SignWithFlags(key, data, flags)
	}
	return c.parent.Sign(key, data)
}

// Changing keys belongs to the agent a command would otherwise have used, so ssh-add keeps
// working the way it did; without one it fails instead of quietly forgetting the key.
var errNoHostAgent = errors.New("agent: no host agent to change keys on")

func (c *chained) Add(key agent.AddedKey) error {
	if c.parent == nil {
		return errNoHostAgent
	}
	return c.parent.Add(key)
}

func (c *chained) Remove(key ssh.PublicKey) error {
	if c.parent == nil {
		return errNoHostAgent
	}
	return c.parent.Remove(key)
}

func (c *chained) RemoveAll() error {
	if c.parent == nil {
		return errNoHostAgent
	}
	return c.parent.RemoveAll()
}

func (c *chained) Lock(passphrase []byte) error {
	if c.parent == nil {
		return errNoHostAgent
	}
	return c.parent.Lock(passphrase)
}

func (c *chained) Unlock(passphrase []byte) error {
	if c.parent == nil {
		return errNoHostAgent
	}
	return c.parent.Unlock(passphrase)
}

func (c *chained) Signers() ([]ssh.Signer, error) { return c.own.Signers() }

func (c *chained) Extension(kind string, body []byte) ([]byte, error) {
	return c.own.Extension(kind, body)
}

// keyring builds the identities one connection can use. Its keys only ever sign.
func keyring(keys []agent.AddedKey) agent.ExtendedAgent {
	ring := agent.NewKeyring()
	for _, key := range keys {
		_ = ring.Add(key)
	}
	extended, _ := ring.(agent.ExtendedAgent)
	return extended
}

func (a *Agent) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	var errs []error
	for owner, live := range a.live {
		errs = append(errs, live.listener.Close())
		delete(a.live, owner)
	}
	return errors.Join(errors.Join(errs...), os.RemoveAll(a.dir))
}
