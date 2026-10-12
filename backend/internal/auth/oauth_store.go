package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SaveOAuthTransaction stores a one-time transaction.
func (s *Store) SaveOAuthTransaction(ctx context.Context, tx OAuthTransaction) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oauth_transaction(id,state_hash,nonce_hash,nonce_text,pkce_verifier_ciphertext,return_path,kind,link_user_id,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		tx.ID, tx.StateHash, tx.NonceHash, tx.Nonce, tx.Verifier, tx.ReturnPath, tx.Kind, tx.LinkUserID, tx.ExpiresAt)
	return err
}

// ConsumeOAuthTransaction validates state once and returns the transaction.
func (s *Store) ConsumeOAuthTransaction(ctx context.Context, stateHash []byte) (OAuthTransaction, error) {
	var tx OAuthTransaction
	err := s.pool.QueryRow(ctx, `SELECT id,state_hash,nonce_hash,COALESCE(nonce_text,''),pkce_verifier_ciphertext,return_path,kind,link_user_id,expires_at FROM oauth_transaction WHERE state_hash=$1 AND consumed_at IS NULL AND expires_at>now()`, stateHash).Scan(
		&tx.ID, &tx.StateHash, &tx.NonceHash, &tx.Nonce, &tx.Verifier, &tx.ReturnPath, &tx.Kind, &tx.LinkUserID, &tx.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OAuthTransaction{}, ErrNotFound
		}
		return OAuthTransaction{}, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE oauth_transaction SET consumed_at=now() WHERE id=$1`, tx.ID); err != nil {
		return OAuthTransaction{}, err
	}
	return tx, nil
}

// CleanupExpiredAuth removes expired oauth/reset rows (daily job).
func (s *Store) CleanupExpiredAuth(ctx context.Context) error {
	now := time.Now()
	for _, q := range []string{
		`DELETE FROM oauth_transaction WHERE expires_at<$1`,
		`DELETE FROM password_reset_token WHERE expires_at<$1 AND consumed_at IS NOT NULL`,
		`DELETE FROM idempotency_key WHERE expires_at<$1`,
	} {
		if _, err := s.pool.Exec(ctx, q, now); err != nil {
			return err
		}
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM auth_session WHERE revoked_at < now() - interval '30 days'`)
	return err
}

// MarkEmailVerified sets verification time for Google-verified emails.
func (s *Store) MarkEmailVerified(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE app_user SET email_verified_at=coalesce(email_verified_at,now()) WHERE id=$1`, userID)
	return err
}

// VerifyNonce checks the stored nonce hash against the presented nonce.
func VerifyNonce(stored []byte, nonce string) bool {
	h := sha256Sum([]byte(nonce))
	if len(h) != len(stored) {
		return false
	}
	for i := range h {
		if h[i] != stored[i] {
			return false
		}
	}
	return true
}
