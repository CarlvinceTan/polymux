package device

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const Version = 1

type Pairing struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Agent        string   `json:"agent"`
	Capabilities []string `json:"capabilities"`
	Revoked      bool     `json:"revoked"`
}

type Enrollment struct {
	Pairing
	Token string `json:"token"`
}

type Store struct {
	access   sync.Mutex
	requests map[*responseGuard]string
	db       *sql.DB
	cert     tls.Certificate
	mu       sync.Mutex
	issuer   *x509.Certificate
	key      *ecdsa.PrivateKey
	clock    func() time.Time
}

func Open(ctx context.Context, dir, hostname string) (*Store, error) {
	dir = filepath.Join(dir, "devices")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "pairing.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS identity(id INTEGER PRIMARY KEY CHECK(id=1), certificate BLOB NOT NULL, key BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS tickets(hash TEXT PRIMARY KEY, agent TEXT NOT NULL, capabilities TEXT NOT NULL, expires INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, name TEXT NOT NULL, agent TEXT NOT NULL, capabilities TEXT NOT NULL, token_hash TEXT UNIQUE NOT NULL, revoked INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS app_lock(caller TEXT PRIMARY KEY, fails INTEGER NOT NULL, until INTEGER NOT NULL);`)
	if err == nil {
		err = s.identity(ctx, hostname)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: s.certificate}
}

func (s *Store) certificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !time.Now().Before(s.issuer.NotAfter) {
		return nil, errors.New("device identity expired; revoke pairings and explicitly reset identity")
	}
	if s.cert.Leaf != nil && time.Until(s.cert.Leaf.NotAfter) > 24*time.Hour {
		cert := s.cert
		return &cert, nil
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Polymux device control"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: minTime(time.Now().Add(90*24*time.Hour), s.issuer.NotAfter),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, DNSNames: s.issuer.DNSNames, IPAddresses: s.issuer.IPAddresses}
	der, err := x509.CreateCertificate(rand.Reader, template, s.issuer, &private.PublicKey, s.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	s.cert = tls.Certificate{Certificate: [][]byte{der, s.issuer.Raw}, PrivateKey: private, Leaf: leaf}
	// Return an immutable generation while a later handshake renews the leaf.
	cert := s.cert
	return &cert, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *Store) identity(ctx context.Context, hostname string) error {
	var certificate, key []byte
	err := s.db.QueryRowContext(ctx, `SELECT certificate,key FROM identity WHERE id=1`).Scan(&certificate, &key)
	if errors.Is(err, sql.ErrNoRows) {
		if ip := net.ParseIP(hostname); ip != nil && ip.IsUnspecified() {
			return errors.New("device identity needs a concrete host")
		}
		private, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return e
		}
		template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Polymux device identity"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(5, 0, 0), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true}
		if ip := net.ParseIP(hostname); ip != nil {
			template.IPAddresses = []net.IP{ip}
		} else {
			template.DNSNames = []string{hostname}
		}
		der, e := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
		if e != nil {
			return e
		}
		pk, e := x509.MarshalPKCS8PrivateKey(private)
		if e != nil {
			return e
		}
		certificate = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		key = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})
		// Concurrent CLI enrollment and server startup share exactly one identity.
		if _, e = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO identity VALUES(1,?,?)`, certificate, key); e != nil {
			return e
		}
		err = s.db.QueryRowContext(ctx, `SELECT certificate,key FROM identity WHERE id=1`).Scan(&certificate, &key)
	}
	if err != nil {
		return err
	}
	identity, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		return err
	}
	s.issuer, err = x509.ParseCertificate(identity.Certificate[0])
	if err != nil {
		return err
	}
	private, ok := identity.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || !s.issuer.IsCA || !time.Now().Before(s.issuer.NotAfter) {
		return errors.New("invalid or expired device identity; explicitly reset it and re-pair")
	}
	s.key = private
	_, err = s.certificate(nil)
	return err
}

func (s *Store) Authenticate(ctx context.Context, token string) (Pairing, error) {
	if len(token) != 43 {
		return Pairing{}, errors.New("invalid device credential")
	}
	var p Pairing
	var caps string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,agent,capabilities,revoked FROM devices WHERE token_hash=? AND revoked=0`, hash(token)).Scan(&p.ID, &p.Name, &p.Agent, &caps, &p.Revoked)
	if err != nil {
		return Pairing{}, errors.New("device revoked or unknown")
	}
	err = json.Unmarshal([]byte(caps), &p.Capabilities)
	return p, err
}

func (s *Store) List(ctx context.Context) ([]Pairing, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,agent,capabilities,revoked FROM devices ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pairing{}
	for rows.Next() {
		var p Pairing
		var caps string
		if err := rows.Scan(&p.ID, &p.Name, &p.Agent, &caps, &p.Revoked); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(caps), &p.Capabilities); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// IssueAppToken creates a named app credential. It is separate from the owner token and can be revoked.
func (s *Store) IssueAppToken(ctx context.Context, name string) (string, error) {
	if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") {
		return "", errors.New("invalid device name")
	}
	token := randomID()
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices(id,name,agent,capabilities,token_hash) VALUES(?,?,?,?,?)`,
		randomID(), name, "local", "[]", hash(token))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) Rename(ctx context.Context, id, name string) error {
	if name == "" || len(name) > 128 || strings.ContainsAny(name, "\x00\r\n") {
		return errors.New("invalid device name")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET name=? WHERE id=? AND revoked=0`, name, id)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n != 1 {
		return errors.New("device not found")
	}
	return err
}

func (s *Store) Revoke(ctx context.Context, id string) error {
	s.access.Lock()
	defer s.access.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET revoked=1 WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("device not found")
	}
	if err == nil {
		for request, device := range s.requests {
			if device == id {
				request.revoke()
			}
		}
	}
	return err
}

// App codes share the tickets table but are not agent-control PairTickets.
const appTicket = `{"kind":"app"}`

var (
	ErrExpired   = &PairError{Code: "pair_expired"}
	ErrUsed      = &PairError{Code: "pair_used"}
	ErrLocked    = &PairError{Code: "pair_locked"}
	ErrNoAddress = &PairError{Code: "no_address"}
)

type PairError struct{ Code string }

func (e *PairError) Error() string { return e.Code }

func (s *Store) IssueApp(ctx context.Context, candidates []string, agent string) (string, error) {
	if agent == "" || len(agent) > 128 || strings.ContainsAny(agent, "/\\.\x00") {
		return "", errors.New("invalid agent")
	}
	seen := map[string]bool{}
	urls := []string{}
	for _, raw := range candidates {
		if target, ok := pairTarget(raw); ok && !seen[target] {
			seen[target] = true
			urls = append(urls, target)
		}
	}
	if len(urls) == 0 {
		return "", ErrNoAddress
	}
	code := randomID()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM tickets WHERE capabilities=? OR expires<=?`, appTicket, s.moment().Unix()); err != nil {
		return "", err
	}
	if err = ticketCap(ctx, tx); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tickets(hash,agent,capabilities,expires) VALUES(?,?,?,?)`, hash(code), agent, appTicket, s.moment().Add(3*time.Minute).Unix())
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(s.issuer.Raw)
	return formatPair(urls, hex.EncodeToString(sum[:]), code, agent), nil
}

// RedeemApp checks the caller lock and consumes the ticket in one transaction.
// Lockout is per caller, so one peer cannot freeze pairing for the others.
func (s *Store) RedeemApp(ctx context.Context, caller, code string) (string, error) {
	if caller == "" || len(caller) > 128 || strings.ContainsRune(caller, 0) {
		return "", errors.New("invalid caller")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_lock(caller,fails,until) VALUES(?,0,0) ON CONFLICT(caller) DO NOTHING`, caller); err != nil {
		return "", err
	}
	var fails int
	var until int64
	if err = tx.QueryRowContext(ctx, `SELECT fails, until FROM app_lock WHERE caller=?`, caller).Scan(&fails, &until); err != nil {
		return "", err
	}
	now := s.moment()
	if until > now.Unix() {
		return "", ErrLocked
	}
	if until != 0 {
		fails = 0
	}
	reason := ErrUsed
	var agent string
	if len(code) == 43 {
		err = tx.QueryRowContext(ctx, `DELETE FROM tickets WHERE hash=? AND capabilities=? AND expires>? RETURNING agent`, hash(code), appTicket, now.Unix()).Scan(&agent)
		if err == nil {
			if _, err = tx.ExecContext(ctx, `UPDATE app_lock SET fails=0, until=0 WHERE caller=?`, caller); err != nil {
				return "", err
			}
			if err = tx.Commit(); err != nil {
				return "", err
			}
			return agent, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		var expires int64
		qerr := tx.QueryRowContext(ctx, `SELECT expires FROM tickets WHERE hash=? AND capabilities=?`, hash(code), appTicket).Scan(&expires)
		if qerr != nil && !errors.Is(qerr, sql.ErrNoRows) {
			return "", qerr
		}
		if qerr == nil && expires <= now.Unix() {
			reason = ErrExpired
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM tickets WHERE expires<=?`, now.Unix()); err != nil {
		return "", err
	}
	fails++
	lockUntil := int64(0)
	if fails >= 5 {
		fails = 0
		lockUntil = now.Add(15 * time.Minute).Unix()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE app_lock SET fails=?, until=? WHERE caller=?`, fails, lockUntil, caller); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return "", reason
}

func (s *Store) moment() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func pairTarget(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || host == "localhost" || polymuxHost(host) {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast()) {
		return "", false
	}
	u.Path = ""
	return u.String(), true
}

func polymuxHost(host string) bool {
	return host == "polymux.com" || strings.HasSuffix(host, ".polymux.com")
}

func formatPair(urls []string, fp, code, agent string) string {
	var b strings.Builder
	b.WriteString("polymux://pair?v=2")
	for _, u := range urls {
		b.WriteString("&u=")
		b.WriteString(url.QueryEscape(u))
	}
	b.WriteString("&fp=")
	b.WriteString(fp)
	b.WriteString("&c=")
	b.WriteString(code)
	b.WriteString("&a=")
	b.WriteString(url.QueryEscape(agent))
	return b.String()
}

// TailscaleURL keeps Serve/Funnel TCP passthrough. Terminated TLS would replace fp.
func TailscaleURL(dnsName string, status []byte, port string) string {
	dnsName = strings.TrimSuffix(strings.TrimSpace(dnsName), ".")
	if dnsName == "" || port == "" || polymuxHost(strings.ToLower(dnsName)) {
		return ""
	}
	var body struct {
		TCP map[string]struct {
			TCPForward, TerminateTLS string
			HTTPS                    bool
		}
	}
	if json.Unmarshal(status, &body) != nil {
		return ""
	}
	for p, h := range body.TCP {
		if h.HTTPS || h.TerminateTLS != "" || !strings.HasSuffix(h.TCPForward, ":"+port) {
			continue
		}
		if p == "443" {
			return "https://" + dnsName
		}
		return "https://" + dnsName + ":" + p
	}
	return ""
}

func randomID() string { return base64.RawURLEncoding.EncodeToString(randBytes(32)) }
func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func ticketCap(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM tickets`).Scan(&count); err != nil {
		return err
	}
	if count >= 64 {
		return errors.New("too many pairing codes")
	}
	return nil
}

func (s *Store) enrollApp(ctx context.Context, caller, code string) (Enrollment, error) {
	agent, err := s.RedeemApp(ctx, caller, code)
	if err != nil {
		return Enrollment{}, err
	}
	p := Enrollment{Pairing: Pairing{ID: randomID(), Name: "App", Agent: agent, Capabilities: []string{}}, Token: randomID()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO devices(id,name,agent,capabilities,token_hash) VALUES(?,?,?,?,?)`, p.ID, p.Name, agent, "[]", hash(p.Token))
	return p, err
}
