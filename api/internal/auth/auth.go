// Package auth implements the single shared-password login and server-side
// sessions.
package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ipedrazas/aifs/api/internal/db"
)

var (
	// ErrInvalidPassword is returned by Login for a wrong password.
	ErrInvalidPassword = errors.New("invalid password")
	// ErrNoSession is returned when a token does not match a live session.
	ErrNoSession = errors.New("no session")
)

// RateLimitedError is returned by Login when too many attempts were made.
type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many login attempts, retry in %s", e.RetryAfter)
}

// Session is a live login.
type Session struct {
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Options configure a Service.
type Options struct {
	Password string
	TTL      time.Duration
	Logger   *slog.Logger
	// Now defaults to time.Now; tests override it.
	Now func() time.Time
	// FingerprintIterations overrides the PBKDF2 cost of the stored password
	// fingerprint. Zero means the production default; tests lower it.
	FingerprintIterations int
}

// Service checks the password and manages sessions.
type Service struct {
	db       *db.DB
	digest   [sha256.Size]byte // SHA-256 of the password, for constant-time compare
	ttl      time.Duration
	iter     int
	log      *slog.Logger
	now      func() time.Time
	attempts *limiter
}

// Login rate limits: per client, 5 attempts then one every 12s (5/minute);
// across all clients, 30 attempts then one every 2s.
var (
	perClientLimit = bucketSpec{burst: 5, every: 12 * time.Second}
	globalLimit    = bucketSpec{burst: 30, every: 2 * time.Second}
)

// New builds the service. It invalidates every existing session when the
// configured password differs from the one the sessions were issued under.
func New(ctx context.Context, database *db.DB, o Options) (*Service, error) {
	if o.Password == "" {
		return nil, errors.New("auth: empty password")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.FingerprintIterations <= 0 {
		o.FingerprintIterations = defaultFingerprintIter
	}
	s := &Service{
		db:       database,
		digest:   sha256.Sum256([]byte(o.Password)),
		ttl:      o.TTL,
		iter:     o.FingerprintIterations,
		log:      o.Logger,
		now:      o.Now,
		attempts: newLimiter(perClientLimit, globalLimit, o.Now),
	}
	if err := s.syncPassword(ctx, o.Password); err != nil {
		return nil, err
	}
	return s, nil
}

// Password fingerprint, stored in app_meta to detect a password change. A
// salted, slow hash so a copy of the database does not reveal the password.
const fingerprintKey = "password_fingerprint"

// defaultFingerprintIter follows the OWASP recommendation for PBKDF2-SHA256.
const defaultFingerprintIter = 600_000

func fingerprint(password string, salt []byte, iter int) (string, error) {
	key, err := pbkdf2.Key(sha256.New, password, salt, iter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iter, hex.EncodeToString(salt), hex.EncodeToString(key)), nil
}

// matchesFingerprint reports whether password produced the stored value,
// using the cost and salt recorded in it.
func matchesFingerprint(password, stored string) (bool, error) {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, nil
	}
	// A malformed value is simply "no match": a new fingerprint replaces it.
	iter, iterErr := strconv.Atoi(parts[1])
	salt, saltErr := hex.DecodeString(parts[2])
	if valid := iterErr == nil && saltErr == nil && iter > 0 && iter <= 10_000_000; !valid {
		return false, nil
	}
	fp, err := fingerprint(password, salt, iter)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(fp), []byte(stored)) == 1, nil
}

func (s *Service) syncPassword(ctx context.Context, password string) error {
	var stored string
	err := s.db.Read.QueryRowContext(ctx, `SELECT value FROM app_meta WHERE key = ?`, fingerprintKey).Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if stored != "" {
		same, err := matchesFingerprint(password, stored)
		if err != nil {
			return err
		}
		if same {
			return nil
		}
	}

	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	fp, err := fingerprint(password, salt, s.iter)
	if err != nil {
		return err
	}
	tx, err := s.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE FROM session`)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, fingerprintKey, fp); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.log.InfoContext(ctx, "password changed; existing sessions revoked", "sessions", n)
	}
	return nil
}

// Login checks the password for a client (identified by clientKey, normally
// its IP) and returns a new session token.
func (s *Service) Login(ctx context.Context, clientKey, password string) (string, Session, error) {
	if ok, wait := s.attempts.allow(clientKey); !ok {
		s.log.WarnContext(ctx, "login rate limited", "client", clientKey)
		return "", Session{}, &RateLimitedError{RetryAfter: wait}
	}

	got := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(got[:], s.digest[:]) != 1 {
		s.log.WarnContext(ctx, "login failed", "client", clientKey)
		return "", Session{}, ErrInvalidPassword
	}

	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // never fails (crypto/rand panics instead)
	token := base64.RawURLEncoding.EncodeToString(raw)

	now := s.now()
	sess := Session{CreatedAt: now.Truncate(time.Second), ExpiresAt: now.Add(s.ttl).Truncate(time.Second)}
	hash := sha256.Sum256([]byte(token))

	if _, err := s.db.Write.ExecContext(ctx, `DELETE FROM session WHERE expires_at <= ?`, now.Unix()); err != nil {
		return "", Session{}, err
	}
	if _, err := s.db.Write.ExecContext(ctx,
		`INSERT INTO session (token_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		hash[:], sess.CreatedAt.Unix(), sess.ExpiresAt.Unix()); err != nil {
		return "", Session{}, err
	}
	s.log.InfoContext(ctx, "login succeeded", "client", clientKey)
	return token, sess, nil
}

// Authenticate returns the live session for a token.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrNoSession
	}
	hash := sha256.Sum256([]byte(token))
	var created, expires int64
	err := s.db.Read.QueryRowContext(ctx,
		`SELECT created_at, expires_at FROM session WHERE token_hash = ? AND expires_at > ?`,
		hash[:], s.now().Unix()).Scan(&created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	return Session{CreatedAt: time.Unix(created, 0), ExpiresAt: time.Unix(expires, 0)}, nil
}

// Logout ends the session for a token. Unknown tokens are not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.Write.ExecContext(ctx, `DELETE FROM session WHERE token_hash = ?`, hash[:])
	return err
}
