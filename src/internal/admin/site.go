package admin

import (
	"net/http"
	"strings"

	"hzk-doc/internal/catalog"
)

type siteNameData struct {
	Name string `json:"name"`
}

func (h *Handler) handleSiteName(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}
	var request siteNameData
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	request.Name = strings.TrimSpace(request.Name)
	if !catalog.ValidSiteName(request.Name) {
		return nil, newAPIError(http.StatusBadRequest, "站点名称需为 1 到 120 个字符，不能包含控制字符")
	}

	h.files.RLock()
	defer h.files.RUnlock()
	h.catalog.Lock()
	err := h.catalog.SetSiteName(request.Name)
	h.catalog.Unlock()
	if err != nil {
		return nil, apiErrorFor(err)
	}
	return request, nil
}
