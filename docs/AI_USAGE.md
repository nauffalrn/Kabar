# Penggunaan AI

## Alat yang dipakai

- **Claude (Anthropic)**, lewat aplikasi Claude dan Claude Code.

## Bagian yang dibantu AI

| Bagian | Peran AI | Peran saya |
|---|---|---|
| Memahami soal dan menentukan deliverable | Memecah soal menjadi daftar output dan keputusan desain | Membaca soal, highlight poin-poin penting, identifikasi masalah, memutuskan apa yang dikerjakan dan bagaimana solusinya |
| Keputusan desain | Memberi opsi beserta trade-off | Memutuskan: REST, PostgreSQL, keyset/cursor, nama author dicocokkan ke semua id, UUIDv7, caching. Menolak rekomendasi awal AI soal search (bagian kegagalan) |
| Kode awal (Go, SQL, Docker Compose) | Menghasilkan sebagian besar kode awal | Membaca dan memahami kode, lalu menjalankan dan memverifikasi setiap alur di mesin sendiri (lihat baris Test dan Validasi performa). Port Postgres di host dibuat bisa dikonfigurasi (`POSTGRES_PORT`) (karena bentrok dengan container lain di mesin saya). Memutuskan perbaikan status 500 → 503 untuk gangguan sementara (bagian keterbatasan) |
| Test | Menghasilkan unit dan integration test | Menjalankan semua test di Windows 11 + Docker Desktop. Unit test: semua lolos. Integration test ke PostgreSQL 16 dan Elasticsearch 8.15.3 sungguhan: semua lolos (7 test Postgres, 3 test Elasticsearch, termasuk pencarian bahasa campuran dan proteksi versi edit). Uji manual semua endpoint dengan `curl`: create author/artikel, list terbaru dulu, search "baca" → "Membaca" dan "read" → "Reading", filter dua author bernama sama, edit + reindex, validasi 400, dan pagination cursor. Semua sesuai |
| Validasi performa | Menyiapkan seeder dan query `EXPLAIN ANALYZE` | Menjalankan seeder 2 juta artikel (30,02% milik satu akun, 40.000 author lain) di mesin sendiri: selesai dalam 2 menit 35 detik. Menjalankan `EXPLAIN ANALYZE` dan mengganti tabel di README bagian 7 dengan angka dari mesin saya. Mengukur latency API: di bawah ~110 ms untuk request pertama dan ~10 ms dari cache, kecuali satu kasus (bagian keterbatasan) |
| Dokumentasi | Draf README dan dokumen ini; memperbarui tabel README dari hasil pengukuran saya | Menyediakan angka pengukuran dari mesin sendiri, menemukan klaim yang ternyata salah ("Kesalahan lain"), dan meminta komentar kode dipersingkat agar kode terbaca dari penamaannya |

## Satu kegagalan AI

**Apa yang terjadi.** Saat diskusi awal, AI merekomendasikan **full-text search bawaan PostgreSQL** (kolom `tsvector` dengan konfigurasi `simple` + GIN index) sebagai mesin pencarian utama. Elasticsearch hanya disebut sebagai "jalur migrasi nanti". Alasan utamanya adalah kesederhanaan setup dan waktu pengerjaan yang singkat.

**Kenapa itu tidak cocok dengan constraint soal.**

1. *Constraint #4 (bahasa campuran).* Konfigurasi `simple` tidak melakukan stemming. Artikel berisi "membaca" tidak akan ditemukan dengan kata kunci "baca", dan "reading" tidak akan ditemukan dengan "read". Ini menurunkan akurasi pencarian yang justru diminta soal.
2. *Constraint #1 dan #6 (50 juta baris, read-heavy, hasil terbaru dulu).* GIN index tidak menyimpan urutan waktu. Untuk keyword yang umum, database harus mengambil semua baris yang cocok lalu mengurutkannya, atau berjalan di index tanggal sambil memfilter. Biaya keduanya tidak terbatas pada skala ini, dan search berjalan di database yang sama dengan write.
3. Alasan "lebih mudah di-setup" bukan alasan teknis terhadap constraint yang diberikan.

**Bagaimana saya menyadarinya.** Saya mempertanyakan apakah pilihan itu diambil karena memang paling baik atau hanya karena setup Elasticsearch lebih rumit. Setelah itu saya minta keputusan dievaluasi ulang berdasarkan merit terhadap setiap constraint, bukan berdasarkan kemudahan.

**Apa yang saya lakukan.** Memakai **Elasticsearch** sebagai read model untuk search:

- multi-field per bahasa (tanpa stemming, `indonesian`, `english`) sehingga tidak perlu deteksi bahasa;
- index sorting `created_at desc` + `track_total_hits: false` untuk *early termination*;
- sinkronisasi lewat **transactional outbox** dengan *external versioning*, sehingga edit artikel tidak pernah hilang atau tertimpa urutan yang salah.

PostgreSQL tetap menjadi source of truth dan melayani list tanpa keyword.

## Kesalahan lain yang tertangkap saat verifikasi

1. **PostgreSQL dan bahasa Indonesia.** Saat menyusun README, AI menulis bahwa PostgreSQL **tidak punya** konfigurasi text search bahasa Indonesia. Klaim ini dicek langsung ke database (`SELECT cfgname FROM pg_ts_config`) dan ternyata **salah**: PostgreSQL 16 punya konfigurasi `indonesian` (`to_tsvector('indonesian', 'membaca dibaca')` menghasilkan `'baca'`). README diperbaiki. Argumen memilih Elasticsearch kini bertumpu pada urutan-plus-skala dan isolasi beban, bukan pada klaim yang keliru itu.

2. **Angka pembanding di README.** README versi awal menyebut query `author_id = ANY(...)` biasa "juga cepat (0,42 ms)" untuk akun yang memegang 30% artikel, sehingga `LATERAL` terkesan hanya jaring pengaman. Saat saya mengukur ulang di 2 juta artikel di mesin sendiri, query itu butuh **1.197 ms**: planner menebak 733 baris, padahal yang dibaca 600.467 baris lalu di-sort hanya untuk mengambil 21 teratas. Versi `LATERAL` yang dipakai aplikasi tetap 0,70 ms. Tabel dan penjelasan di README bagian 7 diganti dengan hasil pengukuran saya.

3. **Ekspektasi hasil tes yang keliru.** Saat menguji edit artikel, AI menyatakan bahwa setelah judul "Reading habits of developers" diganti, pencarian `query=reading` harus kosong. Kenyataannya artikel tetap muncul. Setelah saya telusuri, perilakunya benar: body artikel berisi "read", dan stemmer bahasa Inggris mengubah "reading" menjadi "read". Untuk membuktikan judul lama benar-benar hilang dari index, saya mencari kata yang hanya ada di judul lama (`query=habits`), dan hasilnya kosong.

## Keterbatasan lingkungan dan temuan saat pengujian

- Lingkungan tempat AI membantu membangun kode tidak bisa mengunduh image Elasticsearch. Karena itu, integration test Elasticsearch (`internal/elasticsearch/elasticsearch_integration_test.go`) ditulis di sana tetapi **dijalankan di mesin saya**, dan semuanya lolos tanpa perlu perbaikan.
- `go test -race` tidak bisa dijalankan di mesin Windows saya karena gcc (MinGW) yang terpasang hanya 32-bit, sedangkan race detector butuh cgo 64-bit. Test tanpa `-race` semuanya lolos.
- Port 5432 di mesin saya sudah dipakai container proyek lain, sehingga port Postgres di host dibuat bisa diatur lewat `POSTGRES_PORT` (default tetap 5432).
- Saat mengukur latency API tepat setelah seed 2 juta artikel (indexer masih menyalin sekitar 2 juta dokumen ke Elasticsearch), **satu** request search pertama mengembalikan HTTP 500 setelah 5 detik, sesuai timeout HTTP client ke Elasticsearch. Request berikutnya berhasil. Dengan bantuan AI, saya mencoba mereproduksinya, termasuk saat Elasticsearch sedang menulis ulang 2 juta dokumen, tetapi semua search tetap HTTP 200 dalam ~25 ms. Kemungkinan penyebabnya beban sesaat di laptop. Yang salah di sini bukan hanya lambatnya, tetapi juga **status-nya**: 500 berarti "bug di server", padahal ini gangguan sementara. Saya memutuskan untuk memperbaikinya (implementasi dibantu AI): timeout, koneksi gagal, dan respons 429/502/503/504 dari Elasticsearch sekarang dikembalikan sebagai **503 `unavailable` + header `Retry-After`**, sehingga klien tahu boleh mencoba lagi. Error yang benar-benar tak terduga tetap 500. Perilaku ini dilindungi unit test (`TestErrorMapping`, `TestSearcher_ClassifiesTransientErrors`).
