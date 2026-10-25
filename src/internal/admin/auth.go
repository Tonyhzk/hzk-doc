package admin

import (
	"net"
	"net/http"
	"time"
)

type sessionData struct {
	Enabled       bool   `json:"enabled"`
	Authenticated bool   `json:"authenticated"`
	CSRFToken     string `json:"csrfToken,omitempty"`
}

func (h *Handler) sessionDataFor(r *http.Request) sessionData {
	data := sessionData{Enabled: h.enabled}
	if !h.enabled {
		return data
	}
	if sess, _ := h.sessionFromRequest(r); sess != nil {
		data.Authenticated = true
		data.CSRFToken = sess.csrf
	}
	return data
}

func (h *Handler) handleSession(r *http.Request) (any, *apiError) {
	return h.sessionDataFor(r), nil
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if !h.enabled {
		return nil, newAPIError(http.StatusForbidden, "管理功能未启用")
	}
	if !sameOrigin(r) {
		return nil, newAPIError(http.StatusForbidden, "请求来源无效")
	}

	ip := clientIP(r)

	var request struct {
		Password string `json:"password"`
	}
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if !h.beginLoginAttempt(ip) {
		return nil, newAPIError(http.StatusTooManyRequests, "登录尝试过于频繁")
	}

	if !h.passwordMatches(request.Password) {
		return nil, newAPIError(http.StatusUnauthorized, "密码错误")
	}
	h.clearLoginFailures(ip)

	token, sess, err := h.createSession()
	if err != nil {
		return nil, newAPIError(http.StatusInternalServerError, "服务器内部错误")
	}
	h.setSessionCookie(w, r, token)
	return sessionData{Enabled: true, Authenticated: true, CSRFToken: sess.csrf}, nil
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if !h.enabled {
		return nil, newAPIError(http.StatusForbidden, "管理功能未启用")
	}
	if !sameOrigin(r) {
		return nil, newAPIError(http.StatusForbidden, "请求来源无效")
	}

	sess, key := h.sessionFromRequest(r)
	if sess != nil {
		if !csrfMatches(sess, r.Header.Get("X-CSRF-Token")) {
			return nil, newAPIError(http.StatusForbidden, "CSRF 校验失败")
		}
		h.sessionsMu.Lock()
		delete(h.sessions, key)
		h.sessionsMu.Unlock()
	}
	h.clearSessionCookie(w, r)
	return sessionData{Enabled: true, Authenticated: false}, nil
}

func (h *Handler) sessionFromRequest(r *http.Request) (*session, string) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return nil, ""
	}

	key := hashToken(cookie.Value)
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	sess, ok := h.sessions[key]
	if !ok {
		return nil, key
	}
	if !h.now().Before(sess.expires) {
		delete(h.sessions, key)
		return nil, key
	}
	copied := sess
	return &copied, key
}

func (h *Handler) createSession() (string, session, error) {
	token, err := randomToken()
	if err != nil {
		return "", session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", session{}, err
	}

	now := h.now()
	sess := session{csrf: csrf, expires: now.Add(sessionTTL)}

	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()

	for key, existing := range h.sessions {
		if !now.Before(existing.expires) {
			delete(h.sessions, key)
		}
	}
	if len(h.sessions) >= maxSessions {
		oldestKey := ""
		var oldest time.Time
		for key, existing := range h.sessions {
			if oldestKey == "" || existing.expires.Before(oldest) {
				oldestKey = key
				oldest = existing.expires
			}
		}
		delete(h.sessions, oldestKey)
	}
	h.sessions[hashToken(token)] = sess
	return token, sess, nil
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		Expires:  h.now().Add(sessionTTL),
		HttpOnly: true,
		Secure:   secureCookie(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   secureCookie(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Handler) beginLoginAttempt(ip string) bool {
	now := h.now()
	h.limitsMu.Lock()
	defer h.limitsMu.Unlock()

	h.pruneLimitsLocked(now)

	limit := h.limits[ip]
	if limit != nil && now.Before(limit.blockedUntil) {
		return false
	}
	if limit == nil {
		h.trimLimitsLocked(now, maxLoginClients-1)
		limit = &loginLimit{windowStart: now}
		h.limits[ip] = limit
	} else if !limit.blockedUntil.IsZero() || limit.windowStart.IsZero() ||
		now.Sub(limit.windowStart) >= loginWindow {
		limit.failures = 0
		limit.windowStart = now
		limit.blockedUntil = time.Time{}
	}

	limit.failures++
	if limit.failures >= loginMaxFailures {
		limit.blockedUntil = now.Add(loginBlock)
	}
	return true
}

func (h *Handler) clearLoginFailures(ip string) {
	h.limitsMu.Lock()
	delete(h.limits, ip)
	h.limitsMu.Unlock()
}

func (h *Handler) pruneLimitsLocked(now time.Time) {
	for ip, limit := range h.limits {
		if now.Before(limit.blockedUntil) {
			continue
		}
		if !limit.windowStart.IsZero() && now.Sub(limit.windowStart) < loginWindow {
			continue
		}
		delete(h.limits, ip)
	}
}

func (h *Handler) trimLimitsLocked(now time.Time, size int) {
	if size < 0 {
		size = 0
	}
	for len(h.limits) > size {
		victim := ""
		var victimStart time.Time
		victimBlocked := true
		for ip, limit := range h.limits {
			blocked := now.Before(limit.blockedUntil)
			if victim == "" || (victimBlocked && !blocked) ||
				(victimBlocked == blocked && limit.windowStart.Before(victimStart)) {
				victim = ip
				victimStart = limit.windowStart
				victimBlocked = blocked
			}
		}
		if victim == "" {
			return
		}
		delete(h.limits, victim)
	}
}
