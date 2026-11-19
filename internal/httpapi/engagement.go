package httpapi

import (
	"net/http"
)

func (h *Handler) like(w http.ResponseWriter, r *http.Request) {
	if err := h.Articles.Like(r.Context(), actor(r), r.PathValue("id")); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unlike(w http.ResponseWriter, r *http.Request) {
	if err := h.Articles.Unlike(r.Context(), actor(r), r.PathValue("id")); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listComments(w http.ResponseWriter, r *http.Request) {
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
	page, err := h.Articles.ListComments(r.Context(), r.PathValue("id"), after, limit)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toCommentList(page, false))
}

func (h *Handler) createComment(w http.ResponseWriter, r *http.Request) {
	var req commentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := h.Articles.CreateComment(r.Context(), actor(r), r.PathValue("id"), req.Body)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCommentResponse(c, false))
}

func (h *Handler) updateComment(w http.ResponseWriter, r *http.Request) {
	var req commentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := h.Articles.UpdateComment(r.Context(), actor(r), r.PathValue("id"), req.Body)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toCommentResponse(c, false))
}

func (h *Handler) deleteComment(w http.ResponseWriter, r *http.Request) {
	if err := h.Articles.DeleteComment(r.Context(), actor(r), r.PathValue("id")); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
