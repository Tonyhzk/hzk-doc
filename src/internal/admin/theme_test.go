package admin

import (
	"net/http"
	"testing"
)

func TestThemeWriteRequiresValidAccessAndValue(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, themeData{Theme: "forest"})
	expectStatus(t, env.request(http.MethodPut, "/api/admin/theme", body), http.StatusUnauthorized)
	env.mustLogin()
	expectStatus(t, env.authRequest(http.MethodPut, "/api/admin/theme", body), http.StatusOK)

	tests := []struct {
		body   []byte
		option requestOption
		status int
	}{
		{jsonBody(t, themeData{Theme: "unknown"}), nil, http.StatusBadRequest},
		{jsonBody(t, themeData{}), nil, http.StatusBadRequest},
		{jsonBody(t, themeData{Theme: "mono"}), func(r *http.Request) {
			r.Header.Del("X-CSRF-Token")
		}, http.StatusForbidden},
		{jsonBody(t, themeData{Theme: "mono"}), func(r *http.Request) {
			r.Header.Set("Origin", "http://other.example")
		}, http.StatusForbidden},
	}
	for _, tc := range tests {
		var options []requestOption
		if tc.option != nil {
			options = append(options, tc.option)
		}
		expectStatus(t, env.authRequest(http.MethodPut, "/api/admin/theme", tc.body, options...), tc.status)
		got, err := env.h.catalog.SiteTheme()
		if err != nil || got != "forest" {
			t.Fatalf("被拒绝的请求改变了主题: %q, %v", got, err)
		}
	}
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/theme", nil), http.StatusMethodNotAllowed)
}

func TestThemeWriteWhenManagementDisabled(t *testing.T) {
	env := newTestEnv(t, func(config *Config) { config.Password = "" })
	body := jsonBody(t, themeData{Theme: "mono"})
	expectStatus(t, env.request(http.MethodPut, "/api/admin/theme", body), http.StatusForbidden)
}
