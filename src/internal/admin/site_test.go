package admin

import (
	"net/http"
	"strings"
	"testing"
)

func TestSiteNameWriteRequiresValidAccessAndValue(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, siteNameData{Name: "  测试文档站  "})
	expectStatus(t, env.request(http.MethodPut, "/api/admin/site-name", body), http.StatusUnauthorized)
	env.mustLogin()
	response := env.authRequest(http.MethodPut, "/api/admin/site-name", body)
	expectStatus(t, response, http.StatusOK)
	if got := decodeData[siteNameData](t, response).Name; got != "测试文档站" {
		t.Fatalf("保存的站点名称 = %q", got)
	}

	tests := []struct {
		body   []byte
		option requestOption
		status int
	}{
		{jsonBody(t, siteNameData{Name: "  "}), nil, http.StatusBadRequest},
		{jsonBody(t, siteNameData{Name: strings.Repeat("文", 121)}), nil, http.StatusBadRequest},
		{jsonBody(t, siteNameData{Name: "测试\n站点"}), nil, http.StatusBadRequest},
		{jsonBody(t, siteNameData{Name: "其他站点"}), func(r *http.Request) {
			r.Header.Del("X-CSRF-Token")
		}, http.StatusForbidden},
		{jsonBody(t, siteNameData{Name: "其他站点"}), func(r *http.Request) {
			r.Header.Set("Origin", "http://other.example")
		}, http.StatusForbidden},
	}
	for _, tc := range tests {
		var options []requestOption
		if tc.option != nil {
			options = append(options, tc.option)
		}
		expectStatus(t, env.authRequest(http.MethodPut, "/api/admin/site-name", tc.body, options...), tc.status)
		got, err := env.h.catalog.SiteName()
		if err != nil || got != "测试文档站" {
			t.Fatalf("被拒绝的请求改变了站点名称: %q, %v", got, err)
		}
	}
	longName := strings.Repeat("站", 120)
	expectStatus(t, env.authRequest(http.MethodPut, "/api/admin/site-name", jsonBody(t, siteNameData{Name: longName})), http.StatusOK)
	if got, err := env.h.catalog.SiteName(); err != nil || got != longName {
		t.Fatalf("120 字符名称 = %q, %v", got, err)
	}
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/site-name", nil), http.StatusMethodNotAllowed)
}

func TestSiteNameWriteWhenManagementDisabled(t *testing.T) {
	env := newTestEnv(t, func(config *Config) { config.Password = "" })
	body := jsonBody(t, siteNameData{Name: "测试站点"})
	expectStatus(t, env.request(http.MethodPut, "/api/admin/site-name", body), http.StatusForbidden)
}
