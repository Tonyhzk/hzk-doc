package admin

import (
	"net/http"
	"testing"

	"hzk-doc/internal/catalog"
)

func TestArticleMetaRequiresValidAccessAndValue(t *testing.T) {
	env := newTestEnv(t, nil)
	body := jsonBody(t, articleMetaData{ShowFileType: boolPointer(true), ShowFileSize: boolPointer(true)})
	expectStatus(t, env.request(http.MethodPost, "/api/admin/article-meta", body), http.StatusUnauthorized)
	env.mustLogin()
	response := env.authRequest(http.MethodPost, "/api/admin/article-meta", body)
	expectStatus(t, response, http.StatusOK)
	got := decodeData[articleMetaData](t, response)
	if got.ShowFileType == nil || !*got.ShowFileType || got.ShowFileSize == nil || !*got.ShowFileSize {
		t.Fatalf("返回的文章信息设置 = %+v", got)
	}

	tests := []struct {
		body   []byte
		option requestOption
		status int
	}{
		{[]byte(`{}`), nil, http.StatusBadRequest},
		{[]byte(`{"showFileType":true}`), nil, http.StatusBadRequest},
		{[]byte(`{"showFileSize":true}`), nil, http.StatusBadRequest},
		{[]byte(`{"showFileType":null,"showFileSize":true}`), nil, http.StatusBadRequest},
		{[]byte(`{"showFileType":true,"showFileSize":null}`), nil, http.StatusBadRequest},
		{[]byte(`{"showFileType":"true","showFileSize":true}`), nil, http.StatusBadRequest},
		{[]byte(`{"showFileType":true,"showFileSize":1}`), nil, http.StatusBadRequest},
		{[]byte(`{"showFileType":false,"showFileSize":false}`), func(r *http.Request) {
			r.Header.Del("X-CSRF-Token")
		}, http.StatusForbidden},
		{[]byte(`{"showFileType":false,"showFileSize":false}`), func(r *http.Request) {
			r.Header.Set("Origin", "http://other.example")
		}, http.StatusForbidden},
	}
	for _, tc := range tests {
		var options []requestOption
		if tc.option != nil {
			options = append(options, tc.option)
		}
		expectStatus(t, env.authRequest(http.MethodPost, "/api/admin/article-meta", tc.body, options...), tc.status)
		meta, err := env.h.catalog.ArticleMeta()
		if err != nil || !meta.ShowFileType || !meta.ShowFileSize {
			t.Fatalf("被拒绝的请求改变了文章信息设置: %+v, %v", meta, err)
		}
	}

	for _, meta := range []catalog.ArticleMeta{
		{ShowFileType: true, ShowFileSize: true},
		{ShowFileType: true, ShowFileSize: false},
		{ShowFileType: false, ShowFileSize: true},
		{ShowFileType: false, ShowFileSize: false},
	} {
		response := env.authRequest(http.MethodPost, "/api/admin/article-meta", jsonBody(t, articleMetaData{
			ShowFileType: boolPointer(meta.ShowFileType),
			ShowFileSize: boolPointer(meta.ShowFileSize),
		}))
		expectStatus(t, response, http.StatusOK)
		got := decodeData[articleMetaData](t, response)
		if got.ShowFileType == nil || *got.ShowFileType != meta.ShowFileType ||
			got.ShowFileSize == nil || *got.ShowFileSize != meta.ShowFileSize {
			t.Fatalf("四种组合的返回 = %+v", got)
		}
		saved, err := env.h.catalog.ArticleMeta()
		if err != nil || saved.ShowFileType != meta.ShowFileType || saved.ShowFileSize != meta.ShowFileSize {
			t.Fatalf("四种组合的保存 = %+v, %v", saved, err)
		}
	}
	expectStatus(t, env.authRequest(http.MethodGet, "/api/admin/article-meta", nil), http.StatusMethodNotAllowed)
}

func TestArticleMetaWhenManagementDisabled(t *testing.T) {
	env := newTestEnv(t, func(config *Config) { config.Password = "" })
	body := jsonBody(t, articleMetaData{ShowFileType: boolPointer(true), ShowFileSize: boolPointer(true)})
	expectStatus(t, env.request(http.MethodPost, "/api/admin/article-meta", body), http.StatusForbidden)
	meta, err := env.h.catalog.ArticleMeta()
	if err != nil || meta.ShowFileType || meta.ShowFileSize {
		t.Fatalf("未启用的管理功能改变了文章信息设置: %+v, %v", meta, err)
	}
}
