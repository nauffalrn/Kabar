-- Accounts, categories, images, comments and likes. See docs/ERD.md.

ALTER TABLE authors ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();

CREATE TABLE categories (
    id       text PRIMARY KEY,
    slug     text NOT NULL UNIQUE,
    name     text NOT NULL,
    position int  NOT NULL
);

INSERT INTO categories (id, slug, name, position) VALUES
    ('nasional',   'nasional',   'Nasional',   1),
    ('ekonomi',    'ekonomi',    'Ekonomi',    2),
    ('teknologi',  'teknologi',  'Teknologi',  3),
    ('gaya-hidup', 'gaya-hidup', 'Gaya Hidup', 4),
    ('olahraga',   'olahraga',   'Olahraga',   5);

-- Nullable: articles written before categories existed keep working.
ALTER TABLE articles
    ADD COLUMN category_id text REFERENCES categories (id),
    ADD COLUMN deleted_at  timestamptz;

-- Per-category feed, same shape as the global and per-author indexes.
CREATE INDEX articles_category_created_at_id_idx ON articles (category_id, created_at DESC, id DESC);

CREATE TABLE users (
    id            text PRIMARY KEY,
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    author_id     text        NOT NULL UNIQUE REFERENCES authors (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    disabled_at   timestamptz
);

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

CREATE TABLE sessions (
    id         text PRIMARY KEY,
    user_id    text        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE TABLE article_images (
    id         text PRIMARY KEY,
    article_id text        NOT NULL REFERENCES articles (id),
    position   int         NOT NULL,
    is_cover   boolean     NOT NULL DEFAULT false,
    object_key text        NOT NULL,
    caption    text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Deferred so a reorder can swap positions inside one transaction.
    CONSTRAINT article_images_position_uniq UNIQUE (article_id, position) DEFERRABLE INITIALLY DEFERRED
);

CREATE UNIQUE INDEX article_images_one_cover_idx ON article_images (article_id) WHERE is_cover;

CREATE TABLE comments (
    id         text PRIMARY KEY,
    article_id text        NOT NULL REFERENCES articles (id),
    user_id    text        NOT NULL REFERENCES users (id),
    body       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE INDEX comments_article_created_at_id_idx ON comments (article_id, created_at DESC, id DESC);
CREATE INDEX comments_created_at_id_idx ON comments (created_at DESC, id DESC);

-- The primary key makes a like idempotent: one row per user per article.
CREATE TABLE article_likes (
    article_id text        NOT NULL REFERENCES articles (id),
    user_id    text        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (article_id, user_id)
);

-- Counters live apart from articles so a like never rewrites the large body row.
CREATE TABLE article_stats (
    article_id    text   PRIMARY KEY REFERENCES articles (id),
    like_count    bigint NOT NULL DEFAULT 0,
    comment_count bigint NOT NULL DEFAULT 0
);
