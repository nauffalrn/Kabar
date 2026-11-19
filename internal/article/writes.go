package article

import (
	"context"
	"errors"
	"fmt"
)

func (s *Service) CreateArticle(ctx context.Context, actor *Actor, in CreateArticleInput) (Article, error) {
	if err := requireActor(actor); err != nil {
		return Article{}, err
	}
	in.normalize()
	if err := in.validate(); err != nil {
		return Article{}, err
	}
	categoryID, err := s.resolveCategory(ctx, in.Category)
	if err != nil {
		return Article{}, err
	}

	a := Article{ID: s.d.NewID(), AuthorID: actor.AuthorID, CategoryID: categoryID, Title: in.Title, Body: in.Body}

	// Files are stored before the row exists; a failed insert leaves only
	// unreferenced objects behind, never a row pointing at a missing file.
	images := make([]Image, 0, len(in.Images))
	for i, up := range in.Images {
		img, err := s.storeImage(ctx, a.ID, up)
		if err != nil {
			return Article{}, err
		}
		img.Position = i
		img.IsCover = i == in.CoverIndex
		images = append(images, img)
	}

	created, err := s.d.Writer.CreateArticle(ctx, a, images)
	if err != nil {
		return Article{}, fmt.Errorf("create article: %w", err)
	}
	return created, nil
}

func (s *Service) UpdateArticle(ctx context.Context, actor *Actor, id string, in UpdateArticleInput) (Article, error) {
	if err := s.authorizeArticle(ctx, actor, id); err != nil {
		return Article{}, err
	}
	in.normalize()
	if err := in.validate(); err != nil {
		return Article{}, err
	}
	ch := ArticleChanges{Title: in.Title, Body: in.Body}
	if in.Category != nil {
		categoryID, err := s.resolveCategory(ctx, *in.Category)
		if err != nil {
			return Article{}, err
		}
		ch.CategoryID = &categoryID
	}
	a, err := s.d.Writer.UpdateArticle(ctx, id, ch)
	if err != nil {
		return Article{}, fmt.Errorf("update article: %w", err)
	}
	return a, nil
}

func (s *Service) DeleteArticle(ctx context.Context, actor *Actor, id string) error {
	if err := s.authorizeArticle(ctx, actor, id); err != nil {
		return err
	}
	return s.d.Writer.DeleteArticle(ctx, id)
}

func (s *Service) AddImage(ctx context.Context, actor *Actor, articleID string, up NewImage) (Image, error) {
	if err := s.authorizeArticle(ctx, actor, articleID); err != nil {
		return Image{}, err
	}
	up.Caption = normalizeText(up.Caption)
	if err := validateCaption(up.Caption); err != nil {
		return Image{}, err
	}
	img, err := s.storeImage(ctx, articleID, up)
	if err != nil {
		return Image{}, err
	}
	saved, err := s.d.Images.AddImage(ctx, img, MaxImagesPerArticle)
	if err != nil {
		s.deleteObjects(ctx, img.Key)
		return Image{}, err
	}
	return saved, nil
}

func (s *Service) UpdateImageCaption(ctx context.Context, actor *Actor, imageID, caption string) (Image, error) {
	if _, err := s.authorizeImage(ctx, actor, imageID); err != nil {
		return Image{}, err
	}
	caption = normalizeText(caption)
	if err := validateCaption(caption); err != nil {
		return Image{}, err
	}
	return s.d.Images.UpdateCaption(ctx, imageID, caption)
}

func (s *Service) DeleteImage(ctx context.Context, actor *Actor, imageID string) error {
	if _, err := s.authorizeImage(ctx, actor, imageID); err != nil {
		return err
	}
	img, err := s.d.Images.DeleteImage(ctx, imageID)
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, img.Key)
	return nil
}

func (s *Service) ReorderImages(ctx context.Context, actor *Actor, articleID string, imageIDs []string) error {
	if err := s.authorizeArticle(ctx, actor, articleID); err != nil {
		return err
	}
	return s.d.Images.ReorderImages(ctx, articleID, imageIDs)
}

func (s *Service) SetCover(ctx context.Context, actor *Actor, articleID, imageID string) error {
	if err := s.authorizeArticle(ctx, actor, articleID); err != nil {
		return err
	}
	return s.d.Images.SetCover(ctx, articleID, imageID)
}

func (s *Service) resolveCategory(ctx context.Context, slug string) (string, error) {
	c, err := s.d.Categories.FindCategoryBySlug(ctx, slug)
	if errors.Is(err, ErrCategoryNotFound) {
		return "", NewValidationError("category", "unknown category")
	}
	if err != nil {
		return "", fmt.Errorf("resolve category: %w", err)
	}
	return c.ID, nil
}

// authorizeArticle lets the author or an admin change an article.
func (s *Service) authorizeArticle(ctx context.Context, actor *Actor, articleID string) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	a, err := s.d.Store.GetArticle(ctx, articleID, "")
	if err != nil {
		return err
	}
	if !actor.owns(a.AuthorID) {
		return ErrForbidden
	}
	return nil
}

func (s *Service) authorizeImage(ctx context.Context, actor *Actor, imageID string) (Image, error) {
	if err := requireActor(actor); err != nil {
		return Image{}, err
	}
	img, err := s.d.Images.GetImage(ctx, imageID)
	if err != nil {
		return Image{}, err
	}
	return img, s.authorizeArticle(ctx, actor, img.ArticleID)
}

// storeImage keys objects by article and image id, so deleting one image
// can never remove a file another article still uses.
func (s *Service) storeImage(ctx context.Context, articleID string, up NewImage) (Image, error) {
	if len(up.Data) > MaxImageBytes {
		return Image{}, NewValidationError("images", "each image must be at most 5 MB")
	}
	files, err := s.d.Processor.Process(up.Data)
	if errors.Is(err, ErrUnsupportedImage) {
		return Image{}, NewValidationError("images", "only JPG, PNG or WebP images are allowed")
	}
	if err != nil {
		return Image{}, fmt.Errorf("process image: %w", err)
	}
	img := Image{ID: s.d.NewID(), ArticleID: articleID, Caption: up.Caption}
	img.Key = "articles/" + articleID + "/" + img.ID
	for _, f := range files {
		if err := s.d.Objects.Put(ctx, img.Key+"/"+f.Name, f.ContentType, f.Data); err != nil {
			return Image{}, fmt.Errorf("store image: %w", err)
		}
	}
	return img, nil
}

// deleteObjects is best effort: an orphaned file costs storage, not correctness.
func (s *Service) deleteObjects(ctx context.Context, key string) {
	for _, name := range ImageVariants {
		_ = s.d.Objects.Delete(ctx, key+"/"+name)
	}
}

// ImageVariants are the files ImageProcessor writes for every upload.
var ImageVariants = []string{"large.jpg", "thumb.jpg"}
