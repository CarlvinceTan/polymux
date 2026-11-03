package device

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

const InviteTTL = 3 * time.Minute

// IssueInvite creates an expiring invitation, never an API credential.
func (s *Store) IssueInvite(ctx context.Context) (string, error) {
	return s.IssueGrant(ctx, Grant{})
}

// IssueGrant is IssueInvite for a device whose role, stable ID or name is already known.
func (s *Store) IssueGrant(ctx context.Context, grant Grant) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM invitations WHERE expires<=?`, s.moment().Unix()); err != nil {
		return "", err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM invitations`).Scan(&count); err != nil {
		return "", err
	}
	if count >= 128 {
		return "", errors.New("too many active invitations")
	}
	code := randomID()
	_, err = tx.ExecContext(ctx, `INSERT INTO invitations(hash,expires,role,client,name) VALUES(?,?,?,?,?)`,
		hash(code), s.moment().Add(InviteTTL).Unix(), grant.Role, grant.Client, grant.Name)
	if err == nil {
		err = tx.Commit()
	}
	return code, err
}

// RedeemInvite atomically binds one invitation to a device-chosen credential. Retrying
// the same claim is safe after a lost response; a different device cannot reuse it.
func (s *Store) RedeemInvite(ctx context.Context, code, token string) error {
	if code == token {
		return ErrUsed
	}
	for _, value := range []string{code, token} {
		decoded, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil || len(value) != 43 || len(decoded) != 32 {
			return ErrUsed
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expires int64
	var claim, id string
	var grant Grant
	err = tx.QueryRowContext(ctx, `SELECT expires,claim,device,role,client,name FROM invitations WHERE hash=?`, hash(code)).
		Scan(&expires, &claim, &id, &grant.Role, &grant.Client, &grant.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUsed
	}
	if err != nil {
		return err
	}
	if expires <= s.moment().Unix() {
		return ErrExpired
	}
	if claim != "" {
		var active bool
		if claim != hash(token) {
			return ErrUsed
		}
		if err = tx.QueryRowContext(ctx, `SELECT revoked=0 FROM devices WHERE id=? AND token_hash=?`, id, claim).Scan(&active); err != nil || !active {
			return ErrUsed
		}
		return tx.Commit()
	}
	id = randomID()
	if grant.Name == "" {
		grant.Name = "Polymux app"
	}
	caps, err := capabilities(grant.Role)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO devices(id,name,agent,capabilities,token_hash,paired,client) VALUES(?,?,'local',?,?,1,?)`,
		id, grant.Name, caps, hash(token), grant.Client)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE invitations SET claim=?,device=? WHERE hash=? AND claim=''`, hash(token), id, hash(code)); err != nil {
		return err
	}
	return tx.Commit()
}
