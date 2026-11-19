package httpapi

import (
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"article-service/internal/article"
)

// maxUploadBytes fits five 5 MB images plus the text fields.
const maxUploadBytes = 27 << 20

func (h *Handler) listCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.Articles.ListCategories(r.Context())
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	out := make([]categoryResponse, len(cats))
	for i, c := range cats {
		out[i] = categoryResponse(c)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getAuthor(w http.ResponseWriter, r *http.Request) {
	p, err := h.Articles.GetAuthorProfile(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, profileResponse{ID: p.ID, Name: p.Name, JoinedAt: p.JoinedAt, ArticleCount: p.ArticleCount})
}

// listArticles: GET /articles?query=&author=&author_id=&category=&limit=&cursor=
func (h *Handler) listArticles(w http.ResponseWriter, r *http.Request) {
	q, err := parseListQuery(r)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	page, err := h.Articles.ListArticles(r.Context(), q)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toListResponse(page))
}

// listFeatured: GET /articles/featured?limit= — newest articles with a cover image.
func (h *Handler) listFeatured(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	rows, err := h.Articles.ListFeatured(r.Context(), limit)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toListResponse(article.Page{Articles: rows}))
}

func (h *Handler) getArticle(w http.ResponseWriter, r *http.Request) {
	a, err := h.Articles.GetArticle(r.Context(), r.PathValue("id"), actor(r))
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toArticleDetail(a, actor(r) != nil))
}

// createArticle accepts JSON, or multipart when images are attached.
func (h *Handler) createArticle(w http.ResponseWriter, r *http.Request) {
	var in article.CreateArticleInput
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		form, ok := parseMultipart(w, r)
		if !ok {
			return
		}
		var err error
		if in, err = createInputFromForm(form); err != nil {
			writeDomainError(w, r, h.log, err)
			return
		}
	} else {
		var req createArticleRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		in = article.CreateArticleInput{Title: req.Title, Body: req.Body, Category: req.Category}
	}
	a, err := h.Articles.CreateArticle(r.Context(), actor(r), in)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, toArticleDetail(a, false))
}

func (h *Handler) updateArticle(w http.ResponseWriter, r *http.Request) {
	var req updateArticleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	a, err := h.Articles.UpdateArticle(r.Context(), actor(r), r.PathValue("id"), article.UpdateArticleInput{
		Title: req.Title, Body: req.Body, Category: req.Category,
	})
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toArticleDetail(a, false))
}

func (h *Handler) deleteArticle(w http.ResponseWriter, r *http.Request) {
	if err := h.Articles.DeleteArticle(r.Context(), actor(r), r.PathValue("id")); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) addImage(w http.ResponseWriter, r *http.Request) {
	form, ok := parseMultipart(w, r)
	if !ok {
		return
	}
	files := form.File["image"]
	if len(files) != 1 {
		writeDomainError(w, r, h.log, article.NewValidationError("image", "exactly one image file is required"))
		return
	}
	data, err := readFile(files[0])
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	img, err := h.Articles.AddImage(r.Context(), actor(r), r.PathValue("id"), article.NewImage{Data: data, Caption: first(form.Value["caption"])})
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, toImageResponse(img))
}

func (h *Handler) updateImage(w http.ResponseWriter, r *http.Request) {
	var req updateImageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	img, err := h.Articles.UpdateImageCaption(r.Context(), actor(r), r.PathValue("id"), req.Caption)
	if err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toImageResponse(img))
}

func (h *Handler) deleteImage(w http.ResponseWriter, r *http.Request) {
	if err := h.Articles.DeleteImage(r.Context(), actor(r), r.PathValue("id")); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) reorderImages(w http.ResponseWriter, r *http.Request) {
	var req reorderImagesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Articles.ReorderImages(r.Context(), actor(r), r.PathValue("id"), req.ImageIDs); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setCover(w http.ResponseWriter, r *http.Request) {
	var req setCoverRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Articles.SetCover(r.Context(), actor(r), r.PathValue("id"), req.ImageID); err != nil {
		writeDomainError(w, r, h.log, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseListQuery(r *http.Request) (article.ListQuery, error) {
	v := r.URL.Query()
	q := article.ListQuery{
		Query: v.Get("query"), AuthorName: v.Get("author"), AuthorID: v.Get("author_id"), Category: v.Get("category"),
	}
	limit, err := parseLimit(v.Get("limit"))
	if err != nil {
		return q, err
	}
	q.Limit = limit
	if q.After, err = parseCursor(v.Get("cursor")); err != nil {
		return q, err
	}
	return q, nil
}

func parseLimit(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, article.NewValidationError("limit", "must be between 1 and 100")
	}
	return n, nil
}

func parseCursor(s string) (*article.Cursor, error) {
	if s == "" {
		return nil, nil
	}
	return article.DecodeCursor(s)
}

func parseMultipart(w http.ResponseWriter, r *http.Request) (*multipart.Form, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if _, tooLarge := err.(*http.MaxBytesError); tooLarge || strings.Contains(err.Error(), "too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "upload is too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_form", "request must be multipart/form-data")
		}
		return nil, false
	}
	return r.MultipartForm, true
}

func createInputFromForm(form *multipart.Form) (article.CreateArticleInput, error) {
	in := article.CreateArticleInput{
		Title: first(form.Value["title"]), Body: first(form.Value["body"]), Category: first(form.Value["category"]),
	}
	if s := first(form.Value["cover_index"]); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			return in, article.NewValidationError("cover_index", "must be a number")
		}
		in.CoverIndex = n
	}
	captions := form.Value["captions"]
	for i, fh := range form.File["images"] {
		data, err := readFile(fh)
		if err != nil {
			return in, err
		}
		img := article.NewImage{Data: data}
		if i < len(captions) {
			img.Caption = captions[i]
		}
		in.Images = append(in.Images, img)
	}
	return in, nil
}

func readFile(fh *multipart.FileHeader) ([]byte, error) {
	if fh.Size > article.MaxImageBytes {
		return nil, article.NewValidationError("images", "each image must be at most 5 MB")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, article.MaxImageBytes+1))
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
