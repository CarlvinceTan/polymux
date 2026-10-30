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
	_, err = tx.ExecContext(ctx, `INSERT INTO invitations(hash,expires) VALUES(?,?)`, hash(code), s.moment().Add(InviteTTL).Unix())
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
	err = tx.QueryRowContext(ctx, `SELECT expires,claim,device FROM invitations WHERE hash=?`, hash(code)).Scan(&expires, &claim, &id)
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
	_, err = tx.ExecContext(ctx, `INSERT INTO devices(id,name,agent,capabilities,token_hash,paired) VALUES(?,?,'local','[]',?,1)`, id, "Polymux app", hash(token))
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE invitations SET claim=?,device=? WHERE hash=? AND claim=''`, hash(token), id, hash(code)); err != nil {
		return err
	}
	return tx.Commit()
}
