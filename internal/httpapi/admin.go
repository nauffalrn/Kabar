package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"article-service/internal/storage"
)

func (h *Handler) adminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.Accounts.SearchUsers(r.Context(), actor(r), r.URL.Query().Get("query"))
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	out := make([]userResponse, len(users))
	for i, u := range users {
		out[i] = toUserResponse(u.User)
		count := u.ArticleCount
		out[i].ArticleCount = &count
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) blockUser(w http.ResponseWriter, r *http.Request)   { h.setBlocked(w, r, true) }
func (h *Handler) unblockUser(w http.ResponseWriter, r *http.Request) { h.setBlocked(w, r, false) }

func (h *Handler) setBlocked(w http.ResponseWriter, r *http.Request, blocked bool) {
	if err := h.Accounts.SetBlocked(r.Context(), actor(r), r.PathValue("id"), blocked); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) adminComments(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	after, err := parseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	page, err := h.Articles.ListRecentComments(r.Context(), actor(r), after, limit)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toCommentList(page, true))
}

func (h *Handler) restoreComment(w http.ResponseWriter, r *http.Request) {
	c, err := h.Articles.RestoreComment(r.Context(), actor(r), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toCommentResponse(c, true))
}

// media streams stored images. Keys contain the image id and never change
// content, so browsers and CDNs may cache them for a year.
func (h *Handler) media(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !strings.HasPrefix(key, "articles/") || strings.Contains(key, "..") {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	obj, err := h.Media.Get(r.Context(), key)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	defer obj.Body.Close()
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, obj.Body)
}
