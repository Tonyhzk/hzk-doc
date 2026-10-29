package admin

import (
	"net/http"

	"hzk-doc/internal/catalog"
)

type themeData struct {
	Theme string `json:"theme"`
}

func (h *Handler) handleTheme(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}
	var request themeData
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if !catalog.ValidSiteTheme(request.Theme) {
		return nil, newAPIError(http.StatusBadRequest, "主题无效")
	}

	h.files.RLock()
	defer h.files.RUnlock()
	h.catalog.Lock()
	err := h.catalog.SetSiteTheme(request.Theme)
	h.catalog.Unlock()
	if err != nil {
		return nil, apiErrorFor(err)
	}
	return request, nil
}
