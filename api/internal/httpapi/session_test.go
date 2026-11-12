package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIRequiresSession(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/version"},
		{http.MethodGet, "/api/v1/session"},
		{http.MethodDelete, "/api/v1/session"},
		{http.MethodGet, "/api/v1/blobs/abc"},
		{http.MethodGet, "/api/v1/does-not-exist"},
		{http.MethodPost, "/api/v1/products"},
	} {
		rec := do(t, h, c.method, c.path)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"unauthenticated"`) {
			t.Errorf("%s %s: got %d %s", c.method, c.path, rec.Code, rec.Body)
		}
	}

	// A made-up cookie is no better than none.
	forged := &http.Cookie{Name: "__Host-aifs_session", Value: "forged"}
	if rec := do(t, h, http.MethodGet, "/api/v1/version", forged); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged cookie: got %d", rec.Code)
	}

	// Public routes stay public.
	for _, p := range []string{"/healthz", "/", "/products"} {
		if rec := do(t, h, http.MethodGet, p); rec.Code != http.StatusOK {
			t.Errorf("%s: got %d", p, rec.Code)
		}
	}
}

func TestLoginLogout(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)

	rec := doBody(t, h, http.MethodPost, "/api/v1/session", `{"password":"nope"}`)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "invalid_password") {
		t.Fatalf("wrong password: %d %s", rec.Code, rec.Body)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("cookie set on failed login")
	}

	cookie := login(t, h)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatalf("weak cookie attributes: %+v", cookie)
	}

	rec = do(t, h, http.MethodGet, "/api/v1/session", cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"authenticated":true`) {
		t.Fatalf("session: %d %s", rec.Code, rec.Body)
	}

	rec = do(t, h, http.MethodDelete, "/api/v1/session", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear the cookie: %+v", cleared)
	}

	if rec := do(t, h, http.MethodGet, "/api/v1/version", cookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old cookie still works after logout: %d", rec.Code)
	}
}

func TestLoginBadRequest(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	for _, body := range []string{"", "not json", `{}`, `{"password":""}`, `{"password":"` + strings.Repeat("a", 5000) + `"}`} {
		if rec := doBody(t, h, http.MethodPost, "/api/v1/session", body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %.20q: got %d", body, rec.Code)
		}
	}
}

func TestLoginRateLimited(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	var rec = doBody(t, h, http.MethodPost, "/api/v1/session", `{"password":"guess"}`)
	for range 10 {
		rec = doBody(t, h, http.MethodPost, "/api/v1/session", `{"password":"guess"}`)
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	// The correct password is refused too while limited.
	rec = doBody(t, h, http.MethodPost, "/api/v1/session", `{"password":"`+testPassword+`"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct password while limited: %d", rec.Code)
	}
}

func TestCrossOriginRejected(t *testing.T) {
	h := newServerT(t, fakeDB{}, webFS)
	cookie := login(t, h)

	for _, hdr := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		{"Origin": "https://evil.example"},
	} {
		for _, target := range []struct{ method, path string }{
			{http.MethodPost, "/api/v1/session"},
			{http.MethodDelete, "/api/v1/session"},
		} {
			req := newReq(target.method, target.path, `{"password":"`+testPassword+`"}`)
			req.AddCookie(cookie)
			for k, v := range hdr {
				req.Header.Set(k, v)
			}
			rec := serve(h, req)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cross_origin") {
				t.Errorf("%v %s %s: got %d %s", hdr, target.method, target.path, rec.Code, rec.Body)
			}
		}
	}

	// Same-origin browser requests pass.
	req := newReq(http.MethodDelete, "/api/v1/session", "")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://example.com")
	req.AddCookie(cookie)
	if rec := serve(h, req); rec.Code != http.StatusNoContent {
		t.Errorf("same-origin logout: %d %s", rec.Code, rec.Body)
	}
}
