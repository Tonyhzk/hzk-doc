package admin

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionLoginLogout(t *testing.T) {
	env := newTestEnv(t, nil)

	recorder := env.request(http.MethodGet, "/api/admin/session", nil)
	expectStatus(t, recorder, http.StatusOK)
	data := decodeData[sessionData](t, recorder)
	if !data.Enabled || data.Authenticated || data.CSRFToken != "" {
		t.Fatalf("initial session data = %+v", data)
	}

	recorder = env.login("wrong-password")
	expectStatus(t, recorder, http.StatusUnauthorized)

	recorder = env.login(testPassword)
	expectStatus(t, recorder, http.StatusOK)
	login := decodeData[sessionData](t, recorder)
	if !login.Enabled || !login.Authenticated || len(login.CSRFToken) < 32 {
		t.Fatalf("login data = %+v", login)
	}
	cookie := findCookie(recorder.Result().Cookies(), cookieName)
	if cookie == nil {
		t.Fatal("missing session cookie")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatalf("cookie = %+v", cookie)
	}
	if cookie.Secure {
		t.Fatal("cookie must not be Secure without TLS")
	}

	recorder = env.request(http.MethodGet, "/api/admin/session", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value})
	})
	expectStatus(t, recorder, http.StatusOK)
	withSession := decodeData[sessionData](t, recorder)
	if !withSession.Authenticated || withSession.CSRFToken != login.CSRFToken {
		t.Fatalf("session data = %+v", withSession)
	}

	recorder = env.request(http.MethodPost, "/api/admin/logout", nil,
		func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value})
			r.Header.Set("X-CSRF-Token", login.CSRFToken)
			r.Header.Set("Origin", "http://evil.example")
		})
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.request(http.MethodPost, "/api/admin/logout", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value})
	})
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.request(http.MethodPost, "/api/admin/logout", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value})
		r.Header.Set("X-CSRF-Token", login.CSRFToken)
	})
	expectStatus(t, recorder, http.StatusOK)
	logout := decodeData[sessionData](t, recorder)
	if logout.Authenticated {
		t.Fatalf("logout data = %+v", logout)
	}
	cleared := findCookie(recorder.Result().Cookies(), cookieName)
	if cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("cleared cookie = %+v", cleared)
	}

	recorder = env.request(http.MethodGet, "/api/admin/session", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value})
	})
	expectStatus(t, recorder, http.StatusOK)
	afterLogout := decodeData[sessionData](t, recorder)
	if afterLogout.Authenticated {
		t.Fatal("session still valid after logout")
	}
}

func TestLoginRequiresOriginAndJSON(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, map[string]string{"password": testPassword})

	recorder := env.request(http.MethodPost, "/api/admin/login", body, func(r *http.Request) {
		r.Header.Del("Origin")
	})
	expectStatus(t, recorder, http.StatusForbidden)
	if findCookie(recorder.Result().Cookies(), cookieName) != nil {
		t.Fatal("failed login set a cookie")
	}

	recorder = env.request(http.MethodPost, "/api/admin/login", body, func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.request(http.MethodPost, "/api/admin/login", body, func(r *http.Request) {
		r.Header.Set("Origin", "null")
	})
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.request(http.MethodPost, "/api/admin/login", body, func(r *http.Request) {
		r.Header.Set("Content-Type", "text/plain")
	})
	expectStatus(t, recorder, http.StatusUnsupportedMediaType)

	recorder = env.request(http.MethodPost, "/api/admin/login", []byte("{"))
	expectStatus(t, recorder, http.StatusBadRequest)

	recorder = env.request(http.MethodGet, "/api/admin/login", nil)
	expectStatus(t, recorder, http.StatusMethodNotAllowed)
}

func TestLoginRateLimit(t *testing.T) {
	env := newTestEnv(t, nil)
	for range loginMaxFailures {
		expectStatus(t, env.login("wrong-password"), http.StatusUnauthorized)
	}
	expectStatus(t, env.login(testPassword), http.StatusTooManyRequests)

	recorder := env.request(http.MethodPost, "/api/admin/login",
		jsonBody(t, map[string]string{"password": testPassword}),
		func(r *http.Request) {
			r.RemoteAddr = "203.0.113.9:4321"
		})
	expectStatus(t, recorder, http.StatusOK)
}

func TestLoginRateLimitExpires(t *testing.T) {
	env := newTestEnv(t, nil)
	moment := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	env.h.now = func() time.Time { return moment }

	for range loginMaxFailures {
		expectStatus(t, env.login("wrong-password"), http.StatusUnauthorized)
	}
	expectStatus(t, env.login(testPassword), http.StatusTooManyRequests)

	moment = moment.Add(loginWindow + time.Second)
	expectStatus(t, env.login(testPassword), http.StatusTooManyRequests)

	moment = moment.Add(loginBlock)
	expectStatus(t, env.login(testPassword), http.StatusOK)
}

func TestSessionExpiryAndCapacity(t *testing.T) {
	env := newTestEnv(t, nil)
	moment := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	env.h.now = func() time.Time { return moment }

	recorder := env.login(testPassword)
	expectStatus(t, recorder, http.StatusOK)
	cookie := findCookie(recorder.Result().Cookies(), cookieName)
	if cookie == nil {
		t.Fatal("missing cookie")
	}

	moment = moment.Add(sessionTTL + time.Second)
	recorder = env.request(http.MethodGet, "/api/admin/session", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value})
	})
	expectStatus(t, recorder, http.StatusOK)
	if decodeData[sessionData](t, recorder).Authenticated {
		t.Fatal("expired session is still authenticated")
	}

	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	tokens := make([]string, 0, maxSessions+1)
	for i := 0; i <= maxSessions; i++ {
		moment = base.Add(time.Duration(i) * time.Second)
		recorder = env.login(testPassword)
		expectStatus(t, recorder, http.StatusOK)
		issued := findCookie(recorder.Result().Cookies(), cookieName)
		if issued == nil {
			t.Fatal("missing session cookie during capacity test")
		}
		tokens = append(tokens, issued.Value)
	}

	env.h.sessionsMu.Lock()
	sessionCount := len(env.h.sessions)
	env.h.sessionsMu.Unlock()
	if sessionCount != maxSessions {
		t.Fatalf("session count = %d, want %d", sessionCount, maxSessions)
	}

	recorder = env.request(http.MethodGet, "/api/admin/session", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: tokens[0]})
	})
	expectStatus(t, recorder, http.StatusOK)
	if decodeData[sessionData](t, recorder).Authenticated {
		t.Fatal("oldest session was not evicted")
	}

	recorder = env.request(http.MethodGet, "/api/admin/session", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: tokens[len(tokens)-1]})
	})
	expectStatus(t, recorder, http.StatusOK)
	if !decodeData[sessionData](t, recorder).Authenticated {
		t.Fatal("latest session is not authenticated")
	}
}

func TestCookieSecureOnTLS(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, map[string]string{"password": testPassword})

	req := httptest.NewRequest(http.MethodPost, "https://example.com/api/admin/login", bytes.NewReader(body))
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	env.h.ServeHTTP(recorder, req)
	expectStatus(t, recorder, http.StatusOK)

	cookie := findCookie(recorder.Result().Cookies(), cookieName)
	if cookie == nil || !cookie.Secure {
		t.Fatalf("cookie over TLS = %+v", cookie)
	}

	req = httptest.NewRequest(http.MethodPost, "https://example.com/api/admin/login", bytes.NewReader(body))
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	env.h.ServeHTTP(recorder, req)
	expectStatus(t, recorder, http.StatusForbidden)
}

func TestWriteAccessGuards(t *testing.T) {
	env := newTestEnv(t, nil)

	body := jsonBody(t, folderRequest{Path: "fresh"})
	recorder := env.request(http.MethodPost, "/api/admin/folder", body)
	expectStatus(t, recorder, http.StatusUnauthorized)

	env.mustLogin()
	recorder = env.authRequest(http.MethodPost, "/api/admin/folder", body, func(r *http.Request) {
		r.Header.Del("X-CSRF-Token")
	})
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.authRequest(http.MethodPost, "/api/admin/folder", body, func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.authRequest(http.MethodGet, "/api/admin/entries", nil)
	expectStatus(t, recorder, http.StatusOK)

	recorder = env.request(http.MethodGet, "/api/admin/entries", nil)
	expectStatus(t, recorder, http.StatusUnauthorized)

	recorder = env.authRequest(http.MethodPost, "/api/admin/folder", body)
	expectStatus(t, recorder, http.StatusCreated)
}

func TestDisabledAdmin(t *testing.T) {
	env := newTestEnv(t, func(cfg *Config) {
		cfg.Password = ""
	})

	recorder := env.request(http.MethodGet, "/api/admin/session", nil)
	expectStatus(t, recorder, http.StatusOK)
	data := decodeData[sessionData](t, recorder)
	if data.Enabled || data.Authenticated || data.CSRFToken != "" {
		t.Fatalf("disabled session data = %+v", data)
	}

	recorder = env.request(http.MethodPost, "/api/admin/login",
		jsonBody(t, map[string]string{"password": ""}))
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.request(http.MethodGet, "/api/admin/entries", nil)
	expectStatus(t, recorder, http.StatusForbidden)

	recorder = env.request(http.MethodPost, "/api/admin/logout", nil)
	expectStatus(t, recorder, http.StatusForbidden)
}

func TestNewPasswordPolicy(t *testing.T) {
	docs := t.TempDir()

	handler, err := New(Config{DocsDir: docs, Password: "short"})
	if err == nil {
		_ = handler.Close()
		t.Fatal("short password accepted")
	}
	if strings.Contains(err.Error(), "short") {
		t.Fatalf("error leaked password: %v", err)
	}

	handler, err = New(Config{DocsDir: docs, Password: "123456789012"})
	if err != nil {
		t.Fatalf("12-character password rejected: %v", err)
	}
	_ = handler.Close()

	handler, err = New(Config{DocsDir: docs, Password: ""})
	if err != nil {
		t.Fatalf("empty password rejected: %v", err)
	}
	_ = handler.Close()

	if _, err := New(Config{DocsDir: filepath.Join(docs, "missing"), Password: ""}); err == nil {
		t.Fatal("missing docs directory accepted")
	}
}

func TestLoginRateLimitConcurrent(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, map[string]string{"password": "wrong-password"})

	const attempts = 40
	start := make(chan struct{})
	codes := make(chan int, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- env.request(http.MethodPost, "/api/admin/login", body).Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)

	allowed, limited := 0, 0
	for code := range codes {
		switch code {
		case http.StatusUnauthorized:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("unexpected login status %d", code)
		}
	}
	if allowed != loginMaxFailures || limited != attempts-loginMaxFailures {
		t.Fatalf("allowed=%d limited=%d", allowed, limited)
	}
}

func TestLoginLimitPruneAndCapacity(t *testing.T) {
	env := newTestEnv(t, nil)
	moment := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	env.h.now = func() time.Time { return moment }

	env.h.limits["stale"] = &loginLimit{
		windowStart: moment.Add(-loginWindow - time.Second),
		failures:    1,
	}
	env.h.limits["blocked"] = &loginLimit{
		windowStart:  moment.Add(-loginWindow - time.Second),
		failures:     loginMaxFailures,
		blockedUntil: moment.Add(time.Minute),
	}
	env.h.limits["active"] = &loginLimit{
		windowStart: moment,
		failures:    1,
	}

	if !env.h.beginLoginAttempt("fresh") {
		t.Fatal("fresh attempt rejected")
	}
	if _, ok := env.h.limits["stale"]; ok {
		t.Fatal("expired window entry was not pruned")
	}
	if _, ok := env.h.limits["blocked"]; !ok {
		t.Fatal("active block was pruned")
	}
	if _, ok := env.h.limits["active"]; !ok {
		t.Fatal("active window was pruned")
	}

	moment = moment.Add(loginBlock + time.Second)
	if !env.h.beginLoginAttempt("fresh-2") {
		t.Fatal("attempt after block expiry rejected")
	}
	if _, ok := env.h.limits["blocked"]; ok {
		t.Fatal("expired block was not pruned")
	}

	for i := 0; i < maxLoginClients+10; i++ {
		if !env.h.beginLoginAttempt(fmt.Sprintf("client-%d", i)) {
			t.Fatalf("capacity attempt %d rejected", i)
		}
		if got := len(env.h.limits); got > maxLoginClients {
			t.Fatalf("limit cache size = %d", got)
		}
	}
	if got := len(env.h.limits); got != maxLoginClients {
		t.Fatalf("limit cache size = %d, want %d", got, maxLoginClients)
	}
}

func TestOriginProxyHTTPSAndStrictness(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, map[string]string{"password": testPassword})

	badOrigins := []string{
		"",
		"null",
		"http://evil.example",
		"https://evil.example",
		"https://example.com/",
		"https://example.com/path",
		"https://example.com?x=1",
		"https://example.com/?x=1",
		"https://example.com#frag",
		"https://example.com/#frag",
		"https://user@example.com",
		"ftp://example.com",
		"//example.com",
	}
	for _, origin := range badOrigins {
		req := httptest.NewRequest(http.MethodPost, "http://example.com/api/admin/login", bytes.NewReader(body))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		env.h.ServeHTTP(recorder, req)
		expectStatus(t, recorder, http.StatusForbidden)
		if findCookie(recorder.Result().Cookies(), cookieName) != nil {
			t.Fatalf("invalid origin %q set a cookie", origin)
		}
	}

	missingOrigin := httptest.NewRequest(http.MethodPost, "http://example.com/api/admin/login", bytes.NewReader(body))
	missingOrigin.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	env.h.ServeHTTP(recorder, missingOrigin)
	expectStatus(t, recorder, http.StatusForbidden)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/admin/login", bytes.NewReader(body))
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	env.h.ServeHTTP(recorder, req)
	expectStatus(t, recorder, http.StatusOK)
	login := decodeData[sessionData](t, recorder)
	if !login.Authenticated || login.CSRFToken == "" {
		t.Fatalf("login data = %+v", login)
	}
	cookie := findCookie(recorder.Result().Cookies(), cookieName)
	if cookie == nil || !cookie.Secure {
		t.Fatalf("proxy HTTPS cookie = %+v", cookie)
	}

	logout := httptest.NewRequest(http.MethodPost, "http://example.com/api/admin/logout", nil)
	logout.Header.Set("Origin", "https://example.com")
	logout.AddCookie(&http.Cookie{Name: cookieName, Value: cookie.Value})
	logout.Header.Set("X-CSRF-Token", login.CSRFToken)
	recorder = httptest.NewRecorder()
	env.h.ServeHTTP(recorder, logout)
	expectStatus(t, recorder, http.StatusOK)
	cleared := findCookie(recorder.Result().Cookies(), cookieName)
	if cleared == nil || !cleared.Secure || cleared.MaxAge >= 0 {
		t.Fatalf("cleared proxy cookie = %+v", cleared)
	}

	forwarded := httptest.NewRequest(http.MethodPost, "http://example.com/api/admin/login", bytes.NewReader(body))
	forwarded.Header.Set("Origin", "http://example.com")
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	forwarded.Header.Set("X-Forwarded-Ssl", "on")
	forwarded.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	env.h.ServeHTTP(recorder, forwarded)
	expectStatus(t, recorder, http.StatusOK)
	forwardedCookie := findCookie(recorder.Result().Cookies(), cookieName)
	if forwardedCookie == nil || forwardedCookie.Secure {
		t.Fatalf("forwarded header changed cookie security: %+v", forwardedCookie)
	}
}
