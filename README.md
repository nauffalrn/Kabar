# Article Service

Layanan artikel ringan berbasis **Go**, dengan **PostgreSQL** sebagai *source of truth* dan **Elasticsearch** sebagai *search read model*. Didesain untuk 50 juta artikel (+200 ribu/hari), distribusi author yang sangat timpang, nama author yang tidak unik, konten campuran Indonesia/Inggris, artikel yang bisa diedit, dan trafik baca 100:1 yang *bursty*.

> Bagian penggunaan AI ada di [`docs/AI_USAGE.md`](docs/AI_USAGE.md).

---

## Daftar isi

1. [Menjalankan](#1-menjalankan)
2. [API](#2-api)
3. [Arsitektur](#3-arsitektur)
4. [Keputusan desain per constraint](#4-keputusan-desain-per-constraint)
5. [Kenapa PostgreSQL + Elasticsearch](#5-kenapa-postgresql--elasticsearch)
6. [Model data](#6-model-data)
7. [Bukti: query plan pada data yang timpang](#7-bukti-query-plan-pada-data-yang-timpang)
8. [Konsistensi dan trade-off](#8-konsistensi-dan-trade-off)
9. [Jalur scaling dan extensibility](#9-jalur-scaling-dan-extensibility)
10. [Struktur kode](#10-struktur-kode)
11. [Testing](#11-testing)

---

## 1. Menjalankan

Prasyarat: Docker + Docker Compose. Go 1.24+ hanya diperlukan untuk menjalankan test di luar container.

```bash
docker compose up -d --build        # postgres, elasticsearch, api (:8080), indexer
docker compose run --rm seed -articles 1000000   # opsional: data dummy yang timpang
```

Migrasi database dan pembuatan index Elasticsearch berjalan otomatis saat `api`/`indexer` start (idempoten, aman untuk banyak replika).

Kalau port 5432 di host sudah dipakai, jalankan dengan `POSTGRES_PORT=5433 docker compose up -d --build` (berlaku juga untuk `make test-integration`).

| Perintah | Fungsi |
|---|---|
| `make up` / `make down` | Menyalakan / mematikan stack |
| `make test` | Unit test (tanpa service eksternal) |
| `make test-integration` | Integration test ke Postgres + Elasticsearch (butuh `make up`) |
| `make seed N=5000000` | Mengisi N artikel dummy dengan skew 30% |

Konfigurasi lewat environment variable (lihat `internal/config/config.go`), misalnya `DATABASE_URL`, `ELASTICSEARCH_URL`, `CACHE_TTL`, `DB_MAX_CONNS`, `ES_SHARDS`.

---

## 2. API

Protokol: **REST/JSON**. Untuk dua endpoint utama dengan pola CRUD sederhana, REST paling mudah dikonsumsi, di-cache (HTTP/CDN), dan dites dengan `curl`. gRPC/GraphQL menambah kompleksitas tanpa manfaat nyata di sini.

| Method | Path | Keterangan |
|---|---|---|
| `POST` | `/articles` | **Wajib.** Membuat artikel |
| `GET` | `/articles` | **Wajib.** List artikel, terbaru dulu. Query: `query`, `author`, `limit` (1–100, default 20), `cursor` |
| `PATCH` | `/articles/{id}` | Tambahan: edit `title`/`body` (constraint #5) |
| `POST` | `/authors` | Tambahan: membuat author, karena artikel butuh `author_id` yang valid |
| `GET` | `/healthz` | Health check |

### Contoh

```bash
# Buat author
curl -s -XPOST localhost:8080/authors -d '{"name":"Budi Santoso"}'
# {"id":"0193...","name":"Budi Santoso"}

# Buat artikel
curl -s -XPOST localhost:8080/articles \
  -d '{"author_id":"0193...","title":"Banjir di Jakarta","body":"Flooding hit the city..."}'

# List terbaru, filter author, cari keyword
curl -s 'localhost:8080/articles?author=budi%20santoso&query=banjir&limit=20'
```

Response list:

```json
{
  "data": [
    {
      "id": "0193...",
      "author": { "id": "0193...", "name": "Budi Santoso" },
      "title": "Banjir di Jakarta",
      "body": "Flooding hit the city...",
      "created_at": "2026-11-03T09:32:30.430762Z",
      "updated_at": "2026-11-03T09:32:30.430762Z"
    }
  ],
  "next_cursor": "eyJ0Ijoi..."
}
```

Halaman berikutnya: kirim ulang query yang sama dengan `cursor=<next_cursor>`. `next_cursor: null` berarti sudah halaman terakhir.

### Format error

Semua error memakai bentuk yang sama:

```json
{ "error": { "code": "validation_error", "message": "request is invalid",
             "fields": [ { "field": "title", "message": "is required" } ] } }
```

| Status | `code` | Kapan |
|---|---|---|
| 400 | `validation_error`, `invalid_json`, `invalid_cursor` | Input tidak valid (semua field yang salah dilaporkan sekaligus) |
| 404 | `not_found` | Artikel tidak ada (PATCH) |
| 413 | `body_too_large` | Body > 1 MiB |
| 422 | `author_not_found` | `author_id` tidak ada |
| 500 | `internal_error` | Error tak terduga (detail hanya di log) |
| 503 | `unavailable` | Backend sementara tidak sanggup menjawab (timeout, Elasticsearch overload/tidak bisa dihubungi). Disertai header `Retry-After: 1`, jadi klien boleh mencoba lagi |

---

## 3. Arsitektur

```
                     ┌──────────────────────── API (stateless, bisa banyak replika) ──────────────────────┐
  POST/PATCH ───────►│ httpapi ─► article.Service ─► postgres.ArticleRepo ──┐                             │
                     │                                                      │ 1 transaksi:               │
                     │                                                      │  - tulis articles          │
                     │                                                      │  - tulis article_outbox    │
  GET /articles ────►│ httpapi ─► article.Service ─► cache (singleflight + TTL)                           │
                     │                     │                                                              │
                     │      tanpa `query` ─┼─► postgres.ArticleRepo  (keyset di B-tree)                  │
                     │      dengan `query` ┴─► elasticsearch.Searcher (search_after)                     │
                     └───────────────────────────────────────────────────────────────────────────────────┘
                                   ▲                                         ▲
                              PostgreSQL ──── article_outbox ───► indexer ───► Elasticsearch
                         (source of truth)                    (proses terpisah,   (read model)
                                                               SKIP LOCKED,
                                                               external version)
```

Alur penting:

- **Tulis** hanya ke Postgres. Event untuk search ditulis ke tabel `article_outbox` **dalam transaksi yang sama**. Jadi tidak mungkin "artikel tersimpan tapi index lupa di-update" (*dual-write problem*).
- **Indexer** (proses terpisah) membaca outbox, mengambil **state terbaru** artikel dari Postgres, lalu `_bulk` ke Elasticsearch. Elasticsearch yang lambat atau mati tidak pernah memperlambat atau menggagalkan write di API.
- **Baca** diarahkan ke penyimpanan yang paling cocok:
  - Tanpa keyword: Postgres. Urutan "terbaru dulu" dan filter author dilayani langsung oleh index B-tree, dan konsistensinya kuat (artikel baru langsung terlihat).
  - Dengan keyword: Elasticsearch, yang memang dibangun untuk full-text search multi-bahasa.

---

## 4. Keputusan desain per constraint

### #1 Volume (50 juta, +200 ribu/hari)

- **Keyset pagination**, bukan `OFFSET`. Cursor berisi `(created_at, id)` terakhir. Biayanya sama di halaman 1 maupun halaman ke-50.000 (lihat [bukti](#7-bukti-query-plan-pada-data-yang-timpang): 1,13 ms vs 5.319 ms untuk OFFSET 1 juta).
- **ID UUIDv7**: unik tanpa koordinasi antar replika, dan berurutan waktu sehingga insert selalu masuk ke sisi kanan B-tree (lebih sedikit *page split* dibanding UUIDv4 acak). Kolom tetap bertipe `text` sesuai desain tabel.
- Index Elasticsearch memakai **index sorting** `(created_at desc, id desc)` dan `track_total_hits: false`, sehingga query "terbaru dulu" bisa berhenti setelah mengumpulkan `limit` hasil (*early termination*).
- `limit` dibatasi maksimal 100 dan body request maksimal 1 MiB.

### #2 Author yang timpang (1 akun = 30%)

- Index `(author_id, created_at DESC, id DESC)`: filter author menjadi *range scan* yang berhenti setelah `limit` baris, tidak peduli author itu punya 100 atau 15 juta artikel.
- Untuk nama yang mengarah ke **beberapa** id (lihat #3), query memakai `CROSS JOIN LATERAL`: ambil top-N per author dari index, lalu gabungkan `k × N` baris. Biayanya bergantung pada ukuran halaman, bukan jumlah artikel milik author.
- Di Elasticsearch, dokumen **tidak** di-*route* per author. Routing per `author_id` memang membuat filter author hanya mengenai satu shard, tapi akun 30% akan membuat satu shard jauh lebih besar dari yang lain (*hot shard*). Distribusi default (hash dari `_id`) menjaga shard tetap seimbang.

### #3 Nama author tidak unik — **KEPUTUSAN EKSPLISIT**

> **Parameter `author` mencocokkan nama secara exact, case-insensitive, setelah normalisasi spasi, dan mengembalikan artikel dari SEMUA author yang namanya cocok.**

Contoh: `?author=budi santoso` mengembalikan artikel dari semua author bernama "Budi Santoso", berapa pun jumlah id-nya.

Alasan:

- Parameter yang diminta soal adalah **nama**, jadi kontraknya adalah filter berdasarkan nama. Memilih salah satu id secara diam-diam (misalnya yang pertama) akan memberi hasil yang salah dan tidak bisa dijelaskan ke pengguna.
- Response selalu menyertakan `author.id` **dan** `author.name`, sehingga client tetap bisa membedakan author yang namanya sama.
- *Partial match* (`LIKE '%budi%'`) sengaja tidak dipakai: lebih lambat, ambigu, dan bukan yang diminta. Kalau dibutuhkan, itu fitur "search author" terpisah.

Implementasi: `authors_name_lower_idx` pada `lower(name)` mengubah nama menjadi daftar id (~0,1 ms), lalu daftar id itu dipakai oleh Postgres (`LATERAL`) maupun Elasticsearch (`terms`). Nama yang tidak dikenal langsung mengembalikan list kosong tanpa menyentuh tabel artikel.

Perluasan yang wajar ke depan: parameter `author_id` untuk filter yang presisi. Tidak dibuat sekarang karena tidak diminta (YAGNI).

### #4 Konten campuran Indonesia/Inggris

Bahasa tidak tercatat, dan satu artikel bisa berisi dua bahasa sekaligus. Maka **tidak ada deteksi bahasa**. Setiap field teks diindeks **tiga kali** (*multi-field*):

| Field | Analyzer | Contoh |
|---|---|---|
| `title`, `body` | `standard` + `lowercase` + `asciifolding`, **tanpa stemming** | kata persis dalam bahasa apa pun |
| `title.id`, `body.id` | `indonesian` (stopword + stemmer) | "membaca" ≈ "baca" |
| `title.en`, `body.en` | `english` (stopword + stemmer) | "reading" ≈ "read" |

Query cocok jika cocok pada **salah satu** dari tiga analisis tersebut, dengan `operator: and` + `cross_fields` (semua kata harus muncul, boleh tersebar di title atau body). Hasilnya:

- **Akurat**: kata persis selalu ketemu lewat field utama, jadi stemmer bahasa yang "salah" tidak pernah menghilangkan hasil yang benar.
- **Recall tinggi**: variasi imbuhan di kedua bahasa tetap ketemu.
- Seluruh query berada di **filter context** (tanpa skoring), karena urutan hasil ditentukan oleh tanggal, bukan relevansi. Ini menghemat CPU dan membuat filter bisa di-cache.

### #5 Artikel bisa diedit

- `PATCH /articles/{id}` memperbarui `title`/`body`, `updated_at`, dan menaikkan `version` secara atomik di database. `created_at` tidak pernah berubah (dicek di integration test).
- Setiap edit masuk ke outbox dalam transaksi yang sama, lalu indexer menulis ke Elasticsearch dengan **`version_type: external`** memakai `version` dari Postgres. Event lama yang terlambat atau di-retry **tidak bisa** menimpa versi yang lebih baru (Elasticsearch menolaknya dengan 409, yang diperlakukan sebagai "sudah up to date").
- Banyak edit pada artikel yang sama dalam satu batch digabung menjadi satu penulisan dokumen, karena indexer selalu membaca state terbaru.

### #6 Read-heavy dan bursty

- **Singleflight**: N request identik yang datang bersamaan hanya memicu **satu** query ke backend; sisanya menunggu dan berbagi hasil. Ini mencegah *cache stampede* saat lonjakan.
- **LRU + TTL pendek** (default 2 detik, maksimal 10.000 entri) untuk halaman yang sering diminta (misalnya halaman pertama feed).
- Load yang dibagi dilepas dari pembatalan request pertama (`context.WithoutCancel` + timeout sendiri). Jadi satu client yang disconnect tidak membuat semua client lain ikut gagal.
- **Connection pool** dibatasi (`DB_MAX_CONNS`) dan ada `statement_timeout` di sisi server. Query patologis dimatikan, bukan dibiarkan menumpuk koneksi saat lonjakan.
- Baca tanpa keyword ke Postgres, dengan keyword ke Elasticsearch. Beban search tidak membebani database utama.
- API **stateless**, jadi bisa di-scale horizontal di belakang load balancer.

---

## 5. Kenapa PostgreSQL + Elasticsearch

### PostgreSQL vs MySQL sebagai source of truth

Keduanya sanggup menampung 50 juta baris. Yang menentukan adalah fitur yang langsung dipakai desain ini:

| Kebutuhan | PostgreSQL | MySQL (InnoDB) |
|---|---|---|
| Keyset dengan row comparison `(created_at, id) < ($1, $2)` | Dipakai langsung sebagai kondisi index (terlihat di query plan) | Lazim ditulis ulang manual menjadi `a < x OR (a = x AND b < y)` agar optimizer memakai index dengan andal |
| `LATERAL` untuk top-N per author | Ada | Ada sejak 8.0.14 |
| Index ekspresi `lower(name)` | Ada | Ada sejak 8.0.13 (*functional index*) |
| Antrian outbox `FOR UPDATE SKIP LOCKED` | Ada | Ada sejak 8.0 |
| `timestamptz` (zona waktu eksplisit) | Ada | `TIMESTAMP` terbatas sampai tahun 2038 |

MySQL 8 sebenarnya juga sanggup. Postgres dipilih karena semua kebutuhan di atas tersedia secara langsung dan sudah divalidasi dengan `EXPLAIN ANALYZE` (bagian 7), ditambah `timestamptz`. "MySQL lebih ringan" benar untuk *footprint* awal, tetapi di skala ini yang menentukan adalah perilaku query dan index, bukan ukuran instalasi.

### Elasticsearch vs full-text search bawaan Postgres

Opsi Postgres FTS (`tsvector` + GIN) sempat dipertimbangkan karena lebih sederhana (satu database). Opsi ini ditolak karena:

1. **Bahasa campuran.** Satu kolom `tsvector` hanya punya satu konfigurasi. Konfigurasi `simple` tidak melakukan stemming ("membaca" tidak cocok dengan "baca"). Postgres memang punya konfigurasi `indonesian` dan `english`, sehingga multi-analisis bisa ditiru dengan tiga kolom `tsvector` + tiga GIN index. Tapi itu berarti tiga index besar tambahan di tabel utama yang ikut diperbarui di setiap insert/edit (200 ribu/hari), di database yang juga melayani write.
2. **Keyword + "terbaru dulu" di 50 juta baris.** GIN tidak menyimpan urutan. Untuk keyword yang umum, planner harus memilih antara mengambil *semua* baris yang cocok lalu sort, atau berjalan di index tanggal sambil memfilter. Keduanya tidak terbatas biayanya. Elasticsearch dengan index sorting dan *early termination* dirancang tepat untuk kasus ini.
3. **Isolasi beban.** Search adalah operasi paling mahal. Memindahkannya ke cluster terpisah membuat database utama tetap ringan untuk write dan browse.

Harga yang dibayar: komponen tambahan dan *eventual consistency* (lihat [#8](#8-konsistensi-dan-trade-off)). Ini ditangani dengan transactional outbox, bukan dengan menulis ke dua tempat sekaligus dari API.

Klien Elasticsearch ditulis tipis di atas `net/http` karena hanya butuh tiga endpoint (`PUT index`, `_bulk`, `_search`). Dependensinya lebih kecil, dan setiap request mudah dites dengan `httptest`.

---

## 6. Model data

```sql
authors (id text PK, name text NOT NULL)          -- name TIDAK unik
  INDEX (lower(name))

articles (id text PK, author_id text FK, title text, body text,
          created_at timestamptz, updated_at timestamptz, version bigint)
  INDEX (created_at DESC, id DESC)                 -- feed global
  INDEX (author_id, created_at DESC, id DESC)      -- feed per author (tahan skew)

article_outbox (id bigint identity PK, article_id text, enqueued_at timestamptz)
```

Perubahan dari desain tabel yang diberikan, beserta alasannya:

| Perubahan | Alasan |
|---|---|
| `updated_at` | Artikel bisa diedit (#5); berguna untuk audit dan ditampilkan ke client |
| `version` | Penanda urutan yang monoton untuk *external versioning* di Elasticsearch, supaya edit lama tidak menimpa edit baru |
| `timestamptz` (bukan `timestamp`) | Menyimpan titik waktu absolut, tidak ambigu antar zona waktu server/client |
| Tabel `article_outbox` | Sinkronisasi ke Elasticsearch yang andal (#5) |
| FK `author_id → authors.id` | Integritas data; mencegah artikel tanpa author |

Mapping Elasticsearch ada di `internal/elasticsearch/index.go` (`dynamic: strict`, multi-field per bahasa, index sorting). Aplikasi hanya memakai **alias** `articles`, sehingga perubahan mapping di masa depan bisa dilakukan tanpa downtime: buat `articles_v2`, isi ulang, lalu pindahkan alias.

---

## 7. Bukti: query plan pada data yang timpang

Diukur dengan `cmd/seed` (2 juta artikel, satu akun memegang **30,02%**, 40.000 author di long tail dengan rata-rata ~35 artikel, dan dua author kecil yang namanya sama persis dengan akun sindikasi) di PostgreSQL 16 lewat Docker Desktop di laptop Windows 11, `EXPLAIN (ANALYZE, BUFFERS)`, run kedua (cache hangat):

| # | Kasus | Rencana | Waktu |
|---|---|---|---|
| 1 | Feed global, halaman pertama | Index Scan `articles_created_at_id_idx`, berhenti di 21 baris | 0,89 ms |
| 2 | Feed global, cursor di baris ke-1.000.000 | Index Scan yang sama, mulai dari cursor | **1,13 ms** |
| 3 | `OFFSET 1000000` (pembanding, tidak dipakai) | Membaca dan membuang 1.000.021 baris | **5.319 ms** |
| 4 | `author=Syndication Wire` (3 id, termasuk akun 30%) via `LATERAL` | 3 × Index Scan `articles_author_created_at_id_idx` × 21 baris | **0,70 ms** |
| 5 | Kasus #4 dengan `author_id = ANY(...)` biasa (pembanding) | Planner menebak 733 baris; kenyataannya membaca **600.467** baris lalu sort | **1.197 ms** |
| 6 | Nama dengan 481 author kecil (16.843 artikel) via `LATERAL` | 481 × top-21 dari index, lalu top-N heapsort | 54,6 ms |
| 7 | Kasus #6 dengan `author_id = ANY(...)` biasa (pembanding) | Bitmap scan seluruh 16.843 artikel, lalu sort | 92,2 ms |
| – | Resolusi nama ke id | Bitmap Index Scan `authors_name_lower_idx` | < 0,1 ms |

Yang terlihat dari pengukuran ini:

- **#2 vs #3**: keyset pagination tetap ~1 ms di halaman sedalam apa pun, sedangkan `OFFSET` membaca dan membuang semua baris sebelumnya.
- **#4 vs #5** adalah alasan utama memakai `LATERAL`. Dengan `ANY(...)`, planner tidak tahu bahwa salah satu id memegang 30% data, sehingga memilih bitmap scan yang menarik **semua** 600 ribu artikel akun itu untuk diambil 21 teratas. `LATERAL` membatasi biaya menjadi `k × limit` baris, tidak bergantung pada tebakan planner maupun ukuran author.
- **#6 vs #7**: untuk nama yang dipakai ratusan author kecil, `LATERAL` tetap lebih cepat, tetapi biayanya tumbuh dengan jumlah id (481 × 21 baris). Ini batas yang diketahui (lihat [#8](#8-konsistensi-dan-trade-off)).

Cara mereproduksi: `make seed N=2000000`, lalu jalankan query di atas dengan `EXPLAIN ANALYZE`.

---

## 8. Konsistensi dan trade-off

| Aspek | Perilaku | Kenapa dapat diterima |
|---|---|---|
| Search (dengan `query`) | *Eventually consistent*, biasanya < 1–2 detik (interval polling indexer + refresh Elasticsearch 1 detik) | Wajar untuk search; write tidak pernah hilang berkat outbox |
| Browse (tanpa `query`) | Konsisten kuat di Postgres, ditambah cache maksimal `CACHE_TTL` (2 detik) | TTL pendek dan bisa dikonfigurasi; cache tidak menyimpan error |
| Indexer | *At-least-once*; idempoten berkat external version | Duplikasi aman; urutan terjaga oleh versi |
| Transaksi outbox | Transaksi Postgres terbuka selama satu panggilan `_bulk` | Batch dibatasi (500); jika ini jadi masalah, ganti ke kolom *lease* (`locked_until`) tanpa mengubah interface |
| Commit di luar urutan | Dua transaksi bersamaan bisa commit dengan `created_at` yang sedikit terbalik, sehingga client yang sedang paging tepat di detik itu bisa melewatkan satu artikel | Keterbatasan umum keyset feed; artikel tetap muncul saat feed di-refresh |
| Nama dengan sangat banyak id | Biaya `LATERAL` tumbuh dengan jumlah id (`k × limit`) | 481 id = 54,6 ms; nama yang dipakai ribuan author jarang terjadi |
| Field `body` di list | Dikembalikan utuh | Sesuai soal; untuk payload besar bisa ditambah field `excerpt` |

---

## 9. Jalur scaling dan extensibility

**Kapasitas ke depan** (tidak dibuat sekarang, YAGNI, tapi desainnya sudah siap):

- **Elasticsearch**: `ES_SHARDS` ditentukan dari ukuran data. Patokan umum 10–50 GB per shard; 50 juta artikel dengan body beberapa KB cukup sekitar 5–10 primary shard + 1 replica (replica juga menambah kapasitas baca).
- **Postgres**: *read replica* untuk browse; partisi `articles` per bulan berdasarkan `created_at` saat ukuran tabel atau maintenance (vacuum, arsip) mulai berat.
- **Cache bersama** (Redis) jika jumlah replika API besar dan hit rate cache per proses turun. Cukup buat implementasi lain dari interface `article.PageCache`.
- **CDN / `Cache-Control`** untuk halaman publik saat lonjakan ekstrem.
- **Latensi indexer**: `LISTEN/NOTIFY` untuk memicu indexer segera, menggantikan polling.

**Fitur baru:**

- **Kategori**: tabel `categories` + `article_categories`, field `category_ids` (keyword) di dokumen Elasticsearch, `CategoryIDs` di `ListParams`. Tidak ada perubahan pada handler lain atau pada alur outbox.
- **Komentar**: domain terpisah (`internal/comment`) dengan repository sendiri. Tidak perlu masuk ke Elasticsearch kecuali komentar ingin bisa dicari.
- **Delete artikel**: event outbox dengan tipe, lalu `Sink` menambah `Delete`.
- **Urut berdasarkan relevansi**: parameter `sort=relevance` yang memindahkan klausa keyword dari `filter` ke `must`.

---

## 10. Struktur kode

```
cmd/
  api/        composition root API (satu-satunya tempat yang tahu tipe konkret)
  indexer/    worker outbox -> Elasticsearch
  seed/       generator data dummy yang timpang
internal/
  article/        domain + use case. Tidak bergantung pada Postgres/ES/HTTP
    model.go      entitas, input, validasi
    ports.go      interface yang dibutuhkan service (ArticleWriter, AuthorStore, Reader, PageCache)
    service.go    use case: create/update/list, routing reader, paginasi
    cursor.go     cursor opaque untuk keyset pagination
  postgres/       implementasi ports dengan SQL murni (pgx, tanpa ORM) + migrasi
  elasticsearch/  mapping, search, bulk writer (net/http)
  indexer/        loop worker: batch, idle, exponential backoff
  cache/          singleflight + LRU TTL (generic)
  httpapi/        handler REST, DTO, pemetaan error, middleware
  config/         environment variables
migrations/       file SQL yang di-embed ke binary
```

Penerapan prinsip:

| Prinsip | Wujud di kode |
|---|---|
| **S**ingle Responsibility | Handler hanya urusan HTTP; service hanya aturan bisnis; repository hanya SQL; mapping error ke HTTP hanya ada di `writeDomainError` |
| **O**pen/Closed | Reader baru (misalnya OpenSearch) atau cache baru (Redis) cukup mengimplementasikan interface, tanpa mengubah service |
| **L**iskov Substitution | `postgres.ArticleRepo` dan `elasticsearch.Searcher` sama-sama `article.Reader` dengan kontrak urutan dan cursor yang sama, sehingga service bisa memakai keduanya secara bergantian |
| **I**nterface Segregation | Interface kecil yang didefinisikan oleh pemakainya: `Reader` (1 method), `Sink` (1 method), `httpapi.Service` |
| **D**ependency Inversion | `article` hanya bergantung pada interface; tipe konkret dirakit di `cmd/*/main.go` |
| **DRY** | `articleColumns` + `scanArticle` dipakai semua query; `withTx`; builder validasi; satu fungsi pemetaan error |
| **KISS** | Router standar Go (`net/http`), tanpa framework; klien Elasticsearch tipis; cache in-process |
| **YAGNI** | Tanpa Redis, partisi, delete, atau sort relevansi sampai benar-benar dibutuhkan, tetapi titik ekstensinya sudah ada |

---

## 11. Testing

```bash
make test               # unit test
make up && make test-integration
```

| Lapisan | Yang diuji |
|---|---|
| `article` (unit) | Routing keyword ke Elasticsearch vs Postgres; paginasi `limit+1`; satu nama menjadi banyak id; nama tidak dikenal langsung kosong; validasi; cache key tidak bertabrakan |
| `cache` (unit) | 50 request identik bersamaan = 1 load; TTL; error tidak di-cache; client yang batal tidak membatalkan load bersama |
| `httpapi` (unit) | Parsing parameter, pemetaan setiap error ke status (termasuk 503 + `Retry-After` untuk gangguan sementara), unknown field ditolak, body terlalu besar, recover dari panic |
| `elasticsearch` (unit, `httptest`) | Bentuk query (filter context, `search_after`, `terms`), NDJSON bulk dengan external version, 409 = sukses, kegagalan nyata dilaporkan, timeout/429/503 ditandai sementara sedangkan 400 tidak, `EnsureIndex` idempoten |
| `indexer` (unit) | Batch diteruskan ke sink, gagal = event tetap di outbox, worker terus retry |
| `postgres` (integration) | Insert + outbox dalam satu transaksi, author tidak ada = 422 tanpa sisa outbox, edit parsial + `created_at` tetap, paging tanpa celah/duplikat termasuk timestamp kembar, nama sama beda huruf besar/kecil, outbox menggabungkan duplikat |
| `elasticsearch` (integration) | Pencarian campuran ID/EN (stemming dua bahasa, case-insensitive, AND lintas bahasa), filter author + urutan + `search_after`, versi lama tidak menimpa versi baru |

Semua test memakai library standar `testing`, tanpa framework assertion.
