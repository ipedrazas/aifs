package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ipedrazas/aifs/api/internal/db"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

func setup(t *testing.T) (*db.DB, *clock) {
	t.Helper()
	d, err := db.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, &clock{t: time.Unix(1_800_000_000, 0)}
}

func newService(t *testing.T, d *db.DB, c *clock, password string) *Service {
	t.Helper()
	s, err := New(context.Background(), d, Options{
		Password: password,
		TTL:      time.Hour,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      c.Now,

		FingerprintIterations: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLoginAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	d, c := setup(t)
	s := newService(t, d, c, "correct horse battery")

	if _, _, err := s.Login(ctx, "ip1", "wrong"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("wrong password: got %v", err)
	}

	token, sess, err := s.Login(ctx, "ip1", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 40 || !sess.ExpiresAt.Equal(c.Now().Add(time.Hour)) {
		t.Fatalf("token %q session %+v", token, sess)
	}

	got, err := s.Authenticate(ctx, token)
	if err != nil || !got.ExpiresAt.Equal(sess.ExpiresAt) {
		t.Fatalf("authenticate: %+v %v", got, err)
	}
	if _, err := s.Authenticate(ctx, token+"x"); !errors.Is(err, ErrNoSession) {
		t.Fatalf("tampered token: %v", err)
	}
	if _, err := s.Authenticate(ctx, ""); !errors.Is(err, ErrNoSession) {
		t.Fatalf("empty token: %v", err)
	}
}

func TestTokenIsNotStoredInClear(t *testing.T) {
	ctx := context.Background()
	d, c := setup(t)
	s := newService(t, d, c, "correct horse battery")
	token, _, err := s.Login(ctx, "ip", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.Read.QueryRowContext(ctx, `SELECT count(*) FROM session WHERE token_hash = ?`, []byte(token)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("raw token found in the database")
	}
}

func TestSessionExpires(t *testing.T) {
	ctx := context.Background()
	d, c := setup(t)
	s := newService(t, d, c, "correct horse battery")
	token, _, _ := s.Login(ctx, "ip", "correct horse battery")

	c.Advance(time.Hour - time.Second)
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("still valid: %v", err)
	}
	c.Advance(time.Second)
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestLogout(t *testing.T) {
	ctx := context.Background()
	d, c := setup(t)
	s := newService(t, d, c, "correct horse battery")
	token, _, _ := s.Login(ctx, "ip", "correct horse battery")
	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("session survived logout: %v", err)
	}
	if err := s.Logout(ctx, "unknown"); err != nil {
		t.Fatalf("logout of unknown token: %v", err)
	}
}

func TestPasswordChangeRevokesSessions(t *testing.T) {
	ctx := context.Background()
	d, c := setup(t)
	s := newService(t, d, c, "first password!")
	token, _, _ := s.Login(ctx, "ip", "first password!")

	// Restart with the same password: the session survives.
	s = newService(t, d, c, "first password!")
	if _, err := s.Authenticate(ctx, token); err != nil {
		t.Fatalf("session lost on restart: %v", err)
	}

	// Restart with a new password: the session is revoked.
	s = newService(t, d, c, "second password!")
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("session survived a password change: %v", err)
	}
	if _, _, err := s.Login(ctx, "ip", "first password!"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("old password accepted: %v", err)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	ctx := context.Background()
	d, c := setup(t)
	s := newService(t, d, c, "correct horse battery")

	for i := range 5 {
		if _, _, err := s.Login(ctx, "attacker", "guess"); !errors.Is(err, ErrInvalidPassword) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	_, _, err := s.Login(ctx, "attacker", "correct horse battery")
	var rl *RateLimitedError
	if !errors.As(err, &rl) || rl.RetryAfter <= 0 {
		t.Fatalf("6th attempt not limited: %v", err)
	}

	// Another client is unaffected.
	if _, _, err := s.Login(ctx, "owner", "correct horse battery"); err != nil {
		t.Fatalf("other client blocked: %v", err)
	}

	// After the refill interval the attacker gets one more try.
	c.Advance(perClientLimit.every)
	if _, _, err := s.Login(ctx, "attacker", "guess"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("after refill: %v", err)
	}
}

func TestGlobalRateLimit(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	l := newLimiter(bucketSpec{burst: 5, every: time.Second}, bucketSpec{burst: 3, every: time.Minute}, c.Now)
	for i := range 3 {
		if ok, _ := l.allow(string(rune('a' + i))); !ok {
			t.Fatalf("attempt %d refused", i)
		}
	}
	ok, wait := l.allow("z")
	if ok || wait < 50*time.Second {
		t.Fatalf("global limit not applied: ok=%v wait=%s", ok, wait)
	}
}
