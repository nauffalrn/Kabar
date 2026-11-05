package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"article-service/internal/article"
)

const maxBodyBytes = 1 << 20 // 1 MiB; body is capped at 100k characters anyway

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string               `json:"code"`
	Message string               `json:"message"`
	Fields  []article.FieldError `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: msg}})
}

// writeDomainError is the single place that maps domain errors to HTTP statuses.
func writeDomainError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var verr *article.ValidationError
	switch {
	case errors.As(err, &verr):
		writeJSON(w, http.StatusBadRequest, errorBody{Error: errorDetail{
			Code: "validation_error", Message: "request is invalid", Fields: verr.Fields,
		}})
	case errors.Is(err, article.ErrInvalidCursor):
		writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is invalid")
	case errors.Is(err, article.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "article not found")
	case errors.Is(err, article.ErrAuthorNotFound):
		writeError(w, http.StatusUnprocessableEntity, "author_not_found", "author_id does not exist")
	case errors.Is(err, article.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		// Transient outage: tell clients to retry instead of reporting a server bug.
		log.WarnContext(r.Context(), "backend unavailable", "method", r.Method, "path", r.URL.Path, "err", err)
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "unavailable", "service is temporarily unavailable, retry shortly")
	default:
		log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
	}
}

// decodeJSON limits body size and rejects unknown fields so typos fail loudly.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body is too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		}
		return false
	}
	return true
}
