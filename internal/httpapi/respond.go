package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"

	"article-service/internal/account"
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

type errorMapping struct {
	err    error
	status int
	code   string
	msg    string
}

var errorMappings = []errorMapping{
	{article.ErrInvalidCursor, http.StatusBadRequest, "invalid_cursor", "cursor is invalid"},
	{article.ErrNotFound, http.StatusNotFound, "not_found", "article not found"},
	{article.ErrAuthorNotFound, http.StatusNotFound, "author_not_found", "author not found"},
	{article.ErrImageNotFound, http.StatusNotFound, "image_not_found", "image not found"},
	{article.ErrCommentNotFound, http.StatusNotFound, "comment_not_found", "comment not found"},
	{account.ErrUserNotFound, http.StatusNotFound, "user_not_found", "user not found"},
	{article.ErrTooManyImages, http.StatusUnprocessableEntity, "too_many_images", "an article can hold at most 5 images"},
	{article.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated", "sign in to continue"},
	{account.ErrSessionInvalid, http.StatusUnauthorized, "unauthenticated", "sign in to continue"},
	{account.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect"},
	{article.ErrForbidden, http.StatusForbidden, "forbidden", "you are not allowed to do this"},
	{account.ErrUserDisabled, http.StatusForbidden, "user_blocked", "this account has been blocked"},
	{account.ErrEmailTaken, http.StatusConflict, "email_taken", "this email is already registered"},
}

// writeDomainError is the single place that maps domain errors to HTTP statuses.
func writeDomainError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var verr *article.ValidationError
	if errors.As(err, &verr) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: errorDetail{
			Code: "validation_error", Message: "request is invalid", Fields: verr.Fields,
		}})
		return
	}
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			writeError(w, m.status, m.code, m.msg)
			return
		}
	}
	if errors.Is(err, article.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
		// Transient outage: tell clients to retry instead of reporting a server bug.
		log.WarnContext(r.Context(), "backend unavailable", "method", r.Method, "path", r.URL.Path, "err", err)
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "unavailable", "service is temporarily unavailable, retry shortly")
		return
	}
	log.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
}

// decodeJSON accepts only application/json, limits body size and rejects
// unknown fields so typos fail loudly. Requiring JSON also blocks form-based CSRF.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "request body must be application/json")
		return false
	}
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
