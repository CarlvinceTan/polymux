package sshagent

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func testKey(t *testing.T, comment string) agent.AddedKey {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return agent.AddedKey{PrivateKey: private, Comment: comment}
}

func connect(t *testing.T, a *Agent, owner string) agent.ExtendedAgent {
	t.Helper()
	env := a.Env(owner)
	if env["SSH_AUTH_SOCK"] == "" {
		t.Fatal("no agent socket")
	}
	conn, err := net.Dial("unix", env["SSH_AUTH_SOCK"])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return agent.NewClient(conn)
}

// isolate keeps the host's own agent out of a test that is not about chaining.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("SSH_AUTH_SOCK", "")
}

func TestSignWithoutRevealingTheKey(t *testing.T) {
	isolate(t)
	key := testKey(t, "deploy")
	a, err := Start(func(owner string) []agent.AddedKey {
		if owner == "assistant" {
			return []agent.AddedKey{key}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	client := connect(t, a, "assistant")
	keys, err := client.List()
	if err != nil || len(keys) != 1 || keys[0].Comment != "deploy" {
		t.Fatal("key not offered", keys, err)
	}
	pub, err := ssh.NewPublicKey(key.PrivateKey.(ed25519.PrivateKey).Public())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("polymux signs without showing the key")
	signature, err := client.Sign(pub, data)
	if err != nil {
		t.Fatal("could not sign", err)
	}
	if err := pub.Verify(data, signature); err != nil {
		t.Fatal("signature does not verify", err)
	}
	info, err := os.Stat(a.Env("assistant")["SSH_AUTH_SOCK"])
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket is not private", info, err)
	}
}

func TestEachBotAndConnectionReadsItsKeys(t *testing.T) {
	isolate(t)
	first, second := testKey(t, "first"), testKey(t, "second")
	var mu sync.Mutex
	keys := []agent.AddedKey{first}
	a, err := Start(func(owner string) []agent.AddedKey {
		mu.Lock()
		defer mu.Unlock()
		if owner == "assistant" {
			return keys
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	// A bot with no keys is left exactly as it was, rather than being given an empty agent.
	if a.Env("other") != nil {
		t.Fatal("a bot without keys was given an agent")
	}
	if listed, err := connect(t, a, "assistant").List(); err != nil || len(listed) != 1 {
		t.Fatal("first key missing", listed, err)
	}
	mu.Lock()
	keys = []agent.AddedKey{first, second}
	mu.Unlock()
	if listed, err := connect(t, a, "assistant").List(); err != nil || len(listed) != 2 {
		t.Fatal("newly saved key not offered", listed, err)
	}
}

func TestCloseRemovesSockets(t *testing.T) {
	isolate(t)
	a, err := Start(func(string) []agent.AddedKey { return []agent.AddedKey{testKey(t, "one")} })
	if err != nil {
		t.Fatal(err)
	}
	path := a.Env("assistant")["SSH_AUTH_SOCK"]
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("socket left behind", err)
	}
	if a.Env("assistant") != nil {
		t.Fatal("closed agent still hands out a socket")
	}
}

func TestHostAgentKeysStayAvailable(t *testing.T) {
	isolate(t)
	host, own := testKey(t, "host"), testKey(t, "own")
	parent, err := Start(func(string) []agent.AddedKey { return []agent.AddedKey{host} })
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	t.Setenv("SSH_AUTH_SOCK", parent.Env("anyone")["SSH_AUTH_SOCK"])
	bot, err := Start(func(owner string) []agent.AddedKey {
		if owner == "assistant" {
			return []agent.AddedKey{own}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bot.Close()
	listed, err := connect(t, bot, "assistant").List()
	if err != nil || len(listed) != 2 {
		t.Fatal("bot and host keys are not both offered", listed, err)
	}
	// A bot with no keys keeps whatever the host environment already had.
	if bot.Env("other") != nil {
		t.Fatal("a bot without keys was given a different agent")
	}
}

// hostAgent serves a persistent keyring, the way a real ssh-agent outlives a connection.
func hostAgent(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "host-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		listener.Close()
		os.RemoveAll(dir)
	})
	ring := agent.NewKeyring()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = agent.ServeAgent(ring, conn)
			}()
		}
	}()
	return path
}

func TestAddingKeysKeepsWorkingLikeTheHostAgent(t *testing.T) {
	isolate(t)
	t.Setenv("SSH_AUTH_SOCK", hostAgent(t))
	a, err := Start(func(string) []agent.AddedKey { return []agent.AddedKey{testKey(t, "saved")} })
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := connect(t, a, "assistant").Add(testKey(t, "added")); err != nil {
		t.Fatal("ssh-add no longer works", err)
	}
	// The added key persists in the host agent, as it would have before.
	listed, err := connect(t, a, "assistant").List()
	if err != nil || len(listed) != 2 {
		t.Fatal("added key not kept by the host agent", listed, err)
	}
}

func TestAddingKeysWithoutAHostAgentFails(t *testing.T) {
	isolate(t)
	a, err := Start(func(string) []agent.AddedKey { return []agent.AddedKey{testKey(t, "saved")} })
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	// No agent to add to, so ssh-add reports what it did before Polymux had a socket.
	if err := connect(t, a, "assistant").Add(testKey(t, "added")); err == nil {
		t.Fatal("adding a key to the bot's socket silently succeeded")
	}
}
