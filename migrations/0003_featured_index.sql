-- Newest covers first, for the illustrated strip on the home page. Partial, so it
-- holds one row per illustrated article and the query never walks the
-- (mostly text-only) articles table.
CREATE INDEX article_images_cover_created_at_idx ON article_images (created_at DESC) WHERE is_cover;
