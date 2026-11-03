package hub

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// The ports that say how a server encrypts. Everything else on IMAP is implicit TLS, and on SMTP is STARTTLS.
var imapStartTLS, smtpImplicit = "143", "465"

// mailIdle closes an unused connection before the server does, so the next command never
// finds a socket that only looks alive.
const mailIdle = 5 * time.Minute

// mailConn is one mailbox's held connection. Reconnecting costs seconds a message read
// doesn't, and IMAP carries one command at a time, so mu queues the account's operations.
type mailConn struct {
	mu       sync.Mutex
	client   *imapclient.Client
	timer    *time.Timer
	folder   string
	validity uint32
}

// drop holds c.mu.
func (c *mailConn) drop() {
	if c.timer != nil {
		c.timer.Stop()
	}
	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
	}
	c.folder, c.validity = "", 0
}

func (c *mailConn) idle() {
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(mailIdle, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.drop()
	})
}

func (m *mailbox) conn(id string) *mailConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	value := m.conns[id]
	if value == nil {
		value = &mailConn{}
		m.conns[id] = value
	}
	return value
}

func (m *mailbox) disconnect(id string) {
	value := m.conn(id)
	value.mu.Lock()
	defer value.mu.Unlock()
	value.drop()
}

func (m *mailbox) close() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.conns))
	for id := range m.conns {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.disconnect(id)
	}
}

// connectionLost tells a dead socket from anything the server actually answered.
func connectionLost(client *imapclient.Client, err error) bool {
	var refused *imap.Error
	var public publicError
	if errors.As(err, &refused) || errors.As(err, &public) {
		return false
	}
	var network net.Error
	return client.State() == imap.ConnStateLogout || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) || errors.As(err, &network)
}

// session runs one operation on the account's connection. A held socket can die unnoticed,
// so a lost connection gets one more try on a new one; a server's refusal stays its answer.
func (m *mailbox) session(id string, use func(*imapclient.Client, mailAccount) error) error {
	item, err := m.account(id)
	if err != nil {
		return err
	}
	conn := m.conn(id)
	conn.mu.Lock()
	defer conn.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if conn.client == nil {
			if conn.client, err = m.connect(item); err != nil {
				return err
			}
		}
		err = use(conn.client, item)
		if err != nil && attempt == 0 && connectionLost(conn.client, err) {
			conn.drop()
			continue
		}
		conn.idle()
		return err
	}
}

// dial encrypts from the first byte, or on port 143 straight after the greeting. There is no plain fallback.
func (m *mailbox) dial(address string) (*imapclient.Client, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	config := m.tls
	if config == nil {
		config = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	options := &imapclient.Options{TLSConfig: config, Dialer: &net.Dialer{Timeout: dialTimeout}}
	if port == imapStartTLS {
		return imapclient.DialStartTLS(address, options)
	}
	return imapclient.DialTLS(address, options)
}

func (m *mailbox) connect(item mailAccount) (*imapclient.Client, error) {
	client, err := m.dial(item.IMAP)
	if err != nil {
		return nil, err
	}
	if err := m.authenticate(client, item); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

// authenticate signs in. An expired token is refused just like a wrong password,
// so a refused one is renewed once and no more.
func (m *mailbox) authenticate(client *imapclient.Client, item mailAccount) error {
	if item.Provider == "" {
		return client.Login(item.Email, item.secret.Password).Wait()
	}
	token, err := m.token(context.Background(), item)
	if err == nil {
		err = client.Authenticate(xoauth{user: item.Email, token: token})
	}
	var refused *imap.Error
	if !errors.As(err, &refused) {
		return err
	}
	m.expire(item.ID)
	if token, err = m.token(context.Background(), item); err != nil {
		return err
	}
	return client.Authenticate(xoauth{user: item.Email, token: token})
}

func (m *mailbox) expire(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.access, id)
}
