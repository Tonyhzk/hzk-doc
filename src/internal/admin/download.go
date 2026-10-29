package admin

import (
	"net/http"
)

type downloadData struct {
	DownloadEnabled *bool `json:"downloadEnabled"`
}

func (h *Handler) handleDownload(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}
	var request downloadData
	if apiErr := h.decodeJSON(w, r, maxLoginBody, &request); apiErr != nil {
		return nil, apiErr
	}
	if request.DownloadEnabled == nil {
		return nil, newAPIError(http.StatusBadRequest, "下载开关必须为布尔值")
	}

	h.files.RLock()
	defer h.files.RUnlock()
	h.catalog.Lock()
	err := h.catalog.SetDownloadEnabled(*request.DownloadEnabled)
	h.catalog.Unlock()
	if err != nil {
		return nil, apiErrorFor(err)
	}
	return request, nil
}
