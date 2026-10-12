package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User is the service-level identity record.
type User struct {
	ID            uuid.UUID
	Email         string
	Username      string
	DisplayName   string
	PasswordHash  *string
	EmailVerified *time.Time
	Status        string
	CreatedAt     time.Time
	LoginMethods  []string
}

// Session is an authenticated session row.
type Session struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	TokenHash   []byte
	IdleExpires time.Time
	AbsExpires  time.Time
	LastSeen    time.Time
	Revoked     *time.Time
}

// Store owns all auth SQL. Queries are parameterized; services own transactions.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store. Pool may be nil only in unit tests that never touch DB.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

var ErrNotFound = errors.New("not found")

func scanUser(row pgx.Row) (User, error) {
	var u User
	var hash *string
	var verified *time.Time
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &hash, &verified, &u.Status, &u.CreatedAt)
	if err != nil {
		return User{}, err
	}
	u.PasswordHash = hash
	u.EmailVerified = verified
	return u, nil
}

// CreateUserWithSession inserts user + first session atomically.
func (s *Store) CreateUserWithSession(ctx context.Context, u User, sess Session) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name,password_hash,status) VALUES($1,$2,$3,$4,$5,'active')`,
		u.ID, u.Email, u.Username, u.DisplayName, u.PasswordHash)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_session(id,user_id,token_hash,idle_expires_at,absolute_expires_at,last_seen_at,ip_prefix) VALUES($1,$2,$3,$4,$5,now(),$6)`,
		sess.ID, u.ID, sess.TokenHash, sess.IdleExpires, sess.AbsExpires, sessIP(sess))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func sessIP(sess Session) any { return nil }

// FindUserByIdentifier looks up by email or username (citext, constant shape).
func (s *Store) FindUserByIdentifier(ctx context.Context, identifier string) (User, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,email,username,display_name,password_hash,email_verified_at,status,created_at FROM app_user WHERE email=$1 OR username=$1 LIMIT 1`, identifier)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	methods, _ := s.loginMethods(ctx, u.ID)
	u.LoginMethods = methods
	return u, nil
}

// GetUser fetches by ID with login methods.
func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,email,username,display_name,password_hash,email_verified_at,status,created_at FROM app_user WHERE id=$1`, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	methods, _ := s.loginMethods(ctx, u.ID)
	u.LoginMethods = methods
	return u, nil
}

func (s *Store) loginMethods(ctx context.Context, userID uuid.UUID) ([]string, error) {
	methods := []string{}
	row := s.pool.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE id=$1`, userID)
	var h *string
	if err := row.Scan(&h); err == nil && h != nil {
		methods = append(methods, "password")
	}
	var n int
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM auth_identity WHERE user_id=$1 AND provider='google'`, userID).Scan(&n)
	if n > 0 {
		methods = append(methods, "google")
	}
	return methods, nil
}

// CreateSession inserts a session row.
func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO auth_session(id,user_id,token_hash,idle_expires_at,absolute_expires_at,last_seen_at) VALUES($1,$2,$3,$4,$5,now())`,
		sess.ID, sess.UserID, sess.TokenHash, sess.IdleExpires, sess.AbsExpires)
	return err
}

// GetSessionByHash loads an active session + user.
func (s *Store) GetSessionByHash(ctx context.Context, hash []byte) (Session, User, error) {
	var sess Session
	var u User
	row := s.pool.QueryRow(ctx, `SELECT s.id,s.user_id,s.token_hash,s.idle_expires_at,s.absolute_expires_at,s.last_seen_at,s.revoked_at,
		u.id,u.email,u.username,u.display_name,u.password_hash,u.email_verified_at,u.status,u.created_at
		FROM auth_session s JOIN app_user u ON u.id=s.user_id WHERE s.token_hash=$1`, hash)
	var hash2 *string
	var verified *time.Time
	err := row.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.IdleExpires, &sess.AbsExpires, &sess.LastSeen, &sess.Revoked,
		&u.ID, &u.Email, &u.Username, &u.DisplayName, &hash2, &verified, &u.Status, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, User{}, ErrNotFound
		}
		return Session{}, User{}, err
	}
	u.PasswordHash = hash2
	u.EmailVerified = verified
	methods, _ := s.loginMethods(ctx, u.ID)
	u.LoginMethods = methods
	return sess, u, nil
}

// TouchSession updates last_seen_at at most every 5 minutes (caller checks).
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE auth_session SET last_seen_at=now(), idle_expires_at=now()+interval '24 hours' WHERE id=$1`, id)
	return err
}

// RevokeSession revokes one session.
func (s *Store) RevokeSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE auth_session SET revoked_at=now() WHERE id=$1`, id)
	return err
}

// RevokeAllUserSessions revokes every session for a user.
func (s *Store) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE auth_session SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID)
	return err
}

// UpdateProfile updates username/display name.
func (s *Store) UpdateProfile(ctx context.Context, userID uuid.UUID, username, display string) error {
	_, err := s.pool.Exec(ctx, `UPDATE app_user SET username=$2, display_name=$3, updated_at=now() WHERE id=$1`, userID, username, display)
	return err
}

// SetPassword sets hash and revokes all sessions in one transaction.
func (s *Store) SetPassword(ctx context.Context, userID uuid.UUID, hash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE app_user SET password_hash=$2, updated_at=now() WHERE id=$1`, userID, hash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_session SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreatePasswordReset stores a single-use token hash and invalidates older ones.
func (s *Store) CreatePasswordReset(ctx context.Context, id, userID uuid.UUID, hash []byte, expires time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE password_reset_token SET consumed_at=now() WHERE user_id=$1 AND consumed_at IS NULL`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO password_reset_token(id,user_id,token_hash,expires_at) VALUES($1,$2,$3,$4)`, id, userID, hash, expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ConsumePasswordReset validates and consumes a reset token, returning the user ID.
func (s *Store) ConsumePasswordReset(ctx context.Context, hash []byte) (uuid.UUID, error) {
	var id, userID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id,user_id FROM password_reset_token WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at>now()`, hash).Scan(&id, &userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE password_reset_token SET consumed_at=now() WHERE id=$1`, id); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// FindUserByEmail returns user or ErrNotFound.
func (s *Store) FindUserByEmail(ctx context.Context, email string) (User, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,email,username,display_name,password_hash,email_verified_at,status,created_at FROM app_user WHERE email=$1`, email)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return u, nil
}

// Identity helpers for Google linking.

type Identity struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Provider string
	Subject  string
	Email    *string
}

// FindIdentity locates by provider+subject.
func (s *Store) FindIdentity(ctx context.Context, provider, subject string) (Identity, error) {
	var it Identity
	err := s.pool.QueryRow(ctx, `SELECT id,user_id,provider,provider_subject,provider_email FROM auth_identity WHERE provider=$1 AND provider_subject=$2`, provider, subject).Scan(&it.ID, &it.UserID, &it.Provider, &it.Subject, &it.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, ErrNotFound
		}
		return Identity{}, err
	}
	return it, nil
}

// CreateUserWithIdentity creates a user + google identity + session atomically.
func (s *Store) CreateUserWithIdentity(ctx context.Context, u User, it Identity, sess Session) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO app_user(id,email,username,display_name,email_verified_at,status) VALUES($1,$2,$3,$4,now(),'active')`,
		u.ID, u.Email, u.Username, u.DisplayName)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_identity(id,user_id,provider,provider_subject,provider_email) VALUES($1,$2,'google',$3,$4)`,
		it.ID, u.ID, it.Subject, it.Email)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO auth_session(id,user_id,token_hash,idle_expires_at,absolute_expires_at,last_seen_at) VALUES($1,$2,$3,$4,$5,now())`,
		sess.ID, u.ID, sess.TokenHash, sess.IdleExpires, sess.AbsExpires)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// LinkIdentity attaches a provider identity to an existing user.
func (s *Store) LinkIdentity(ctx context.Context, it Identity) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO auth_identity(id,user_id,provider,provider_subject,provider_email) VALUES($1,$2,$3,$4,$5)`,
		it.ID, it.UserID, it.Provider, it.Subject, it.Email)
	return err
}

// UniqueUsername derives a collision-safe username from a base.
func (s *Store) UniqueUsername(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 0; i < 20; i++ {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM app_user WHERE username=$1`, candidate).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return candidate, nil
		}
		candidate = base + "_" + uuid.NewString()[:4]
	}
	return "user_" + uuid.NewString()[:8], nil
}
