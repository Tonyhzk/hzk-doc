package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"hzk-doc/internal/content"
)

type orderRequest struct {
	Directory string   `json:"directory"`
	Names     []string `json:"names"`
}

type orderData struct {
	Directory string   `json:"directory"`
	Names     []string `json:"names"`
}

func (h *Handler) handleOrder(w http.ResponseWriter, r *http.Request) (any, *apiError) {
	if _, apiErr := h.requireWriteAccess(r); apiErr != nil {
		return nil, apiErr
	}

	var request orderRequest
	if apiErr := h.decodeJSON(w, r, maxOrderBytes, &request); apiErr != nil {
		return nil, apiErr
	}
	if err := content.ValidatePath(request.Directory, true); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "目录无效")
	}

	seen := make(map[string]bool, len(request.Names))
	for _, name := range request.Names {
		if err := validateOrderName(name); err != nil {
			return nil, newAPIError(http.StatusBadRequest, "排序名称无效")
		}
		if seen[name] {
			return nil, newAPIError(http.StatusBadRequest, "排序名称重复")
		}
		seen[name] = true
	}

	names := append([]string{}, request.Names...)

	apiErr := h.mutate(func() *apiError {
		entries, err := h.listEntries(request.Directory)
		if err != nil {
			return apiErrorFor(err)
		}
		if len(entries) != len(seen) {
			return newAPIError(http.StatusBadRequest, "排序列表不完整")
		}
		for _, entry := range entries {
			if !seen[entry.Name] {
				return newAPIError(http.StatusBadRequest, "排序列表不完整")
			}
		}

		order, err := h.loadOrder()
		if err != nil {
			return newAPIError(http.StatusInternalServerError, "排序数据不可用")
		}
		order[request.Directory] = names
		if err := h.saveOrder(order); err != nil {
			return apiErrorFor(err)
		}
		return nil
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return orderData{Directory: request.Directory, Names: names}, nil
}

func validateOrderName(name string) error {
	if name == "" || name == "." || name == ".." {
		return errInvalidName
	}
	if strings.HasPrefix(name, ".") {
		return errInvalidName
	}
	if strings.ContainsAny(name, "/\\\x00:") {
		return errInvalidName
	}
	if path.Base(name) != name {
		return errInvalidName
	}
	if strings.TrimSpace(name) != name {
		return errInvalidName
	}
	return nil
}

func (h *Handler) loadOrder() (map[string][]string, error) {
	info, err := h.root.Lstat(content.OrderFile)
	if errors.Is(err, fs.ErrNotExist) {
		return make(map[string][]string), nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, errSymlink
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	if info.Size() > maxOrderBytes {
		return nil, errTooLarge
	}

	data, err := h.root.ReadFile(content.OrderFile)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return make(map[string][]string), nil
	}

	order := make(map[string][]string)
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, errMalformedOrder
	}
	if order == nil {
		order = make(map[string][]string)
	}
	h.catalog.ResolveOrder(order)
	return order, nil
}

func (h *Handler) saveOrder(order map[string][]string) error {
	data, err := json.MarshalIndent(order, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxOrderBytes {
		return errTooLarge
	}

	tmp, err := h.tempName(".", ".hzk-order-tmp-")
	if err != nil {
		return err
	}
	file, err := h.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = h.root.Remove(tmp)
		}
	}()

	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := h.root.Rename(tmp, content.OrderFile); err != nil {
		return err
	}
	remove = false
	return nil
}
