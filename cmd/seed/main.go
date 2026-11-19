// Command seed loads skewed synthetic data (one account ~30%, long tail,
// duplicate names, mixed ID/EN text) to validate query plans.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"article-service/internal/postgres"
	"article-service/migrations"
)

const syndicationName = "Syndication Wire"

var categories = []string{"nasional", "ekonomi", "teknologi", "gaya-hidup", "olahraga"}

var (
	firstNames = []string{"Budi", "Siti", "Agus", "Dewi", "Andi", "Rina", "John", "Maria", "Eko", "Putri"}
	lastNames  = []string{"Santoso", "Wijaya", "Pratama", "Lestari", "Smith", "Hidayat", "Saputra", "Nugroho"}
	words      = strings.Fields(`pemerintah ekonomi pasar saham harga beras banjir jakarta teknologi startup
		pendidikan sekolah membaca berita olahraga sepak bola pemilu kebijakan investasi digital
		government economy market stock price rice flood technology education reading news football
		election policy investment digital climate health vaccine inflation bank rupiah energy`)
)

func main() {
	nArticles := flag.Int("articles", 1_000_000, "number of articles")
	share := flag.Float64("syndication-share", 0.30, "share of articles owned by the syndication account")
	days := flag.Int("days", 250, "spread created_at over this many past days")
	enqueue := flag.Bool("enqueue", true, "enqueue every article in the outbox so the indexer indexes it")
	flag.Parse()

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgres.PoolConfig{URL: env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/articles?sslmode=disable")})
	must(err)
	defer pool.Close()
	must(postgres.Migrate(ctx, pool, migrations.FS))

	// Long tail sized so that the average non-syndication author has ~35 articles.
	nTail := max(1, int(float64(*nArticles)*(1-*share)/35))
	authors := makeAuthors(nTail)
	_, err = pool.CopyFrom(ctx, pgx.Identifier{"authors"}, []string{"id", "name"},
		pgx.CopyFromSlice(len(authors), func(i int) ([]any, error) { return []any{authors[i][0], authors[i][1]}, nil }))
	must(err)
	log.Printf("authors: %d (syndication + %d tail, incl. duplicate names)", len(authors), nTail)

	start := time.Now()
	src := &articleSource{total: *nArticles, share: *share, days: *days, authors: authors, now: time.Now()}
	n, err := pool.CopyFrom(ctx, pgx.Identifier{"articles"},
		[]string{"id", "author_id", "category_id", "title", "body", "created_at", "updated_at"}, src)
	must(err)
	log.Printf("articles: %d in %s", n, time.Since(start).Round(time.Second))

	if *enqueue {
		_, err = pool.Exec(ctx, "INSERT INTO article_outbox (article_id) SELECT id FROM articles")
		must(err)
	}
	_, err = pool.Exec(ctx, "ANALYZE authors; ANALYZE articles")
	must(err)
	log.Print("done")
}

// makeAuthors returns [id, name] pairs; index 0 is the syndication account.
func makeAuthors(nTail int) [][2]string {
	authors := [][2]string{{newID(), syndicationName}}
	for i := 0; i < nTail; i++ {
		name := firstNames[rand.IntN(len(firstNames))] + " " + lastNames[rand.IntN(len(lastNames))]
		if i < 2 {
			name = syndicationName
		}
		authors = append(authors, [2]string{newID(), name})
	}
	return authors
}

// articleSource streams rows into COPY without holding them all in memory.
type articleSource struct {
	total, i, days int
	share          float64
	authors        [][2]string
	now            time.Time
}

func (s *articleSource) Next() bool { s.i++; return s.i <= s.total }
func (s *articleSource) Err() error { return nil }

func (s *articleSource) Values() ([]any, error) {
	authorID := s.authors[0][0]
	if rand.Float64() >= s.share {
		authorID = s.authors[1+rand.IntN(len(s.authors)-1)][0]
	}
	created := s.now.Add(-time.Duration(rand.Int64N(int64(s.days) * int64(24*time.Hour))))
	category := categories[rand.IntN(len(categories))]
	return []any{newID(), authorID, category, sentence(6), sentence(30), created, created}, nil
}

func sentence(n int) string {
	w := make([]string, n)
	for i := range w {
		w[i] = words[rand.IntN(len(words))]
	}
	return strings.Join(w, " ")
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
