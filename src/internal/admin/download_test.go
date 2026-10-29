package admin

import (
	"net/http"
	"testing"
)

func boolPointer(value bool) *bool {
	return &value
}

func TestDownloadToggleRequiresValidAccessAndValue(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, downloadData{DownloadEnabled: boolPointer(true)})
	expectStatus(t, env.request(http.MethodPost, "/api/admin/download", body), http.StatusUnauthorized)
	env.mustLogin()
	response := env.authRequest(http.MethodPost, "/api/admin/download", body)
	expectStatus(t, response, http.StatusOK)
	if got := decodeData[downloadData](t, response).DownloadEnabled; got == nil || !*got {
		t.Fatalf("返回的下载开关 = %v", got)
	}
	if enabled, err := env.h.catalog.DownloadEnabled(); err != nil || !enabled {
		t.Fatalf("保存的下载开关 = %v, %v", enabled, err)
	}

	tests := []struct {
		body   []byte
		option requestOption
		status int
	}{
		{[]byte(`{}`), nil, http.StatusBadRequest},
		{[]byte(`{"downloadEnabled":null}`), nil, http.StatusBadRequest},
		{[]byte(`{"downloadEnabled":"true"}`), nil, http.StatusBadRequest},
		{[]byte(`{"downloadEnabled":1}`), nil, http.StatusBadRequest},
		{jsonBody(t, downloadData{DownloadEnabled: boolPointer(false)}), func(r *http.Request) {
			r.Header.Del("X-CSRF-Token")
		}, http.StatusForbidden},
		{jsonBody(t, downloadData{DownloadEnabled: boolPointer(false)}), func(r *http.Request) {
			r.Header.Set("Origin", "http://other.example")
		}, http.StatusForbidden},
	}
	for _, tc := range tests {
		var options []requestOption
		if tc.option != nil {
			options = append(options, tc.option)
		}
		expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/download", tc.body, options...), tc.status)
		if enabled, err := env.h.catalog.DownloadEnabled(); err != nil || !enabled {
			t.Fatalf("被拒绝的请求改变了下载开关: %v, %v", enabled, err)
		}
	}

	response = env.authRequest(http.MethodPost, "/api/admin/download", jsonBody(t, downloadData{DownloadEnabled: boolPointer(false)}))
	expectStatus(t, response, http.StatusOK)
	if got := decodeData[downloadData](t, response).DownloadEnabled; got == nil || *got {
		t.Fatalf("关闭后返回的下载开关 = %v", got)
	}
	if enabled, err := env.h.catalog.DownloadEnabled(); err != nil || enabled {
		t.Fatalf("关闭后的下载开关 = %v, %v", enabled, err)
	}
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/download", nil), http.StatusMethodNotAllowed)
}

func TestDownloadToggleWhenManagementDisabled(t *testing.T) {
	env := newTestEnv(t, func(config *Config) { config.Password = "" })
	body := jsonBody(t, downloadData{DownloadEnabled: boolPointer(true)})
	expectStatus(t, env.request(http.MethodPost, "/api/admin/download", body), http.StatusForbidden)
	if enabled, err := env.h.catalog.DownloadEnabled(); err != nil || enabled {
		t.Fatalf("未启用的管理功能改变了下载开关: %v, %v", enabled, err)
	}
}
