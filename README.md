# IF3110-lahtetibabanget
Tugas Besar IF3110 Pengembangan Aplikasi Web 2026

# Database Management Platform

Platform web untuk mengelola akses dan perubahan schema database bersama (PostgreSQL, dengan
dukungan tambahan MySQL/MariaDB): akses diatur per grup, struktur dan data dapat dijelajahi, dan
setiap perubahan dilakukan lewat migration yang tercatat.

## Fitur

- **Autentikasi dan sesi**: login username/password (bcrypt), session acak yang disimpan sebagai hash,
  cookie HttpOnly + SameSite (+ Secure di production), idle timeout, absolute expiration, rotasi
  berkala dengan grace period, dan perlindungan CSRF.
- **Otorisasi**: user → grup → akses database (*View Only* / *View + Edit*), dicek di backend pada
  setiap request. Platform Admin mengelola user, grup, koneksi, dan akses, tetapi bukan level akses database.
- **Koneksi database**: CRUD koneksi PostgreSQL, MySQL, dan MariaDB oleh admin. Engine dipilih saat
  koneksi dibuat dan tidak dapat diubah. Password dienkripsi (AES-256-GCM), ada uji koneksi, dan
  kegagalan koneksi ditangani dengan halaman error yang aman.
- **Database viewer**: schema, tabel, kolom, constraint, index, dan data (read-only, paginasi).
- **Migration**: SQL up/down atau irreversible dengan alasan, revisi immutable, review dan approval
  leader setiap grup, eksekusi asynchronous lewat antrean dan worker (mode biasa dan per batch dengan
  checkpoint), rollback dan forward, riwayat semua attempt dengan Hide rollback, serta pemulihan
  ketika worker mati.
- **Analisis execution plan (bonus)**: EXPLAIN dan EXPLAIN ANALYZE per statement di halaman review,
  ditampilkan sebagai pohon plan. Semua perubahan di-rollback. Khusus PostgreSQL.
- **Schema diff (bonus)**: membandingkan schema database target dengan DDL kondisi yang diinginkan,
  menampilkan perbedaan struktur, lalu menghasilkan kandidat SQL migration yang dapat diedit dan
  disimpan sebagai draft migration biasa. DDL hanya dijalankan sementara di schema terpisah yang
  selalu di-rollback, sehingga database target tidak berubah.
- **Dukungan DBMS tambahan (bonus)**: koneksi, access control, database viewer, dan migration juga
  berjalan pada MySQL dan MariaDB. Perbedaan perilaku (terutama DDL yang tidak transactional)
  dijelaskan di bagian "Keterbatasan yang diketahui".

## Teknologi

Go (backend dan worker), PostgreSQL 16 (platform DB, antrean job, dan target), MariaDB 11 (target
tambahan), SPA React + TypeScript + Vite + Tailwind, pgx, go-sql-driver/mysql, OpenTelemetry, Docker
Compose.

## Prasyarat

Go (versi sesuai `go.mod`), Docker + Docker Compose, `make`, dan `openssl`.
Di beberapa mesin Docker membutuhkan `sudo`.

## Menjalankan

```bash
make up                                    # platform-db :5434, test-db :5433, target-db (sampel PG) :5435, target-mariadb (sampel) :3307
make web-build                             # build SPA (sekali, atau setelah frontend berubah)
ADMIN_PASSWORD='ganti-password' make run   # run pertama membuat akun admin
make run-worker                            # terminal lain; atau pakai service worker dari make up
```

Buka http://localhost:8080 dan login sebagai `admin` dengan password tadi. Schema platform
dibuat otomatis saat server mulai. `make down` mematikan container; data tetap tersimpan di
volume. Untuk menghapus semua data (termasuk akun admin) jalankan `docker compose down -v`.

`make run` membaca file `.env` bila ada (lihat `.env.example`; jangan di-commit).

### SPA

SPA ada di `web/` (Vite, React, TypeScript, Tailwind) dan butuh Node.js 20 atau lebih baru.

```bash
make web-build # build ke web/dist, hasilnya di-embed ke binary Go
make run # buka http://localhost:8080/app/
```

Saat development, jalankan `make web-dev` di terminal lain lalu buka http://localhost:5173/app/.
Vite meneruskan `/api` ke `:8080` sehingga cookie sesi tetap satu origin.

Halaman koneksi, viewer, dan persetujuan memakai API JSON yang sama dengan server:

| Halaman | Rute SPA | API |
|---|---|---|
| Koneksi (admin) | `/app/admin/koneksi`, `/app/admin/koneksi/baru`, `/app/admin/koneksi/{id}` | `/api/admin/connections` dan `/api/admin/connections/test` |
| Database | `/app/database`, `/app/database/{id}` | `/api/databases` dan `/api/databases/{id}` |
| Viewer tabel | `/app/database/{id}?schema=...&table=...&page=...` | `/api/databases/{id}/tables/{schema}/{table}` |
| Persetujuan leader | `/app/persetujuan` | `/api/approvals?mine=true`, `/api/migrations/{id}/approve`, `/api/migrations/{id}/reject` |

Form koneksi memiliki tombol uji koneksi; password tidak pernah dikirim kembali ke browser. Viewer
menampilkan NULL, sel terpotong, paginasi, dan scroll horizontal. Database target yang mati
mengarahkan ke halaman `/app/error/503`.

#### Pemeriksaan responsif (27 November 2026)

Seluruh rute SPA dibuka lewat Chrome headless pada lebar 360 px dan 1280 px, masing-masing dengan
pengguna yang sesuai (anonim, admin, editor, leader, viewer only) dan data nyata: migration sukses,
gagal, rollback, forward, batch 2 juta baris, menunggu persetujuan, dan draft. Untuk setiap halaman
diperiksa apakah dokumen melebar melebihi layar (`scrollWidth > innerWidth`), error JavaScript, dan
request API yang gagal, lalu diambil screenshot.

Hasil: 32 rute × 2 lebar, tidak ada halaman yang melebar dan tidak ada error JavaScript. Satu-satunya
respons gagal adalah `401 /api/me` di halaman login, yang memang dipakai SPA untuk mengetahui bahwa
pengguna belum login. Tabel data viewer, tabel riwayat, dan editor SQL bergulir di dalam komponennya
sendiri pada 360 px. Rute yang diperiksa: login, sesi berakhir, beranda, koneksi (daftar, tambah,
ubah), user (daftar, tambah, ubah), grup (daftar, tambah, detail), database (daftar, detail, viewer
tabel), migration (daftar, buat, ubah draft, review), riwayat (dengan dan tanpa Hide rollback),
detail eksekusi, schema diff, monitor job (sukses dan gagal), persetujuan leader, dan halaman error.

### Persetujuan migration

Setiap eksekusi migration harus disetujui leader dari setiap grup yang punya akses ke database tujuan.

- Grup tanpa leader aktif menahan proses.
- Pengaju tidak boleh menyetujui permintaannya sendiri.
- Persetujuan terikat pada revisi. Bila migration diubah, persetujuan lama tidak berlaku.
- Satu penolakan menolak permintaan.

### Konfigurasi (environment)

| Variabel | Default | Keterangan |
|---|---|---|
| `APP_ADDR` | `:8080` | alamat server |
| `APP_ENV` | `development` | `production` mengaktifkan cookie `Secure` (butuh HTTPS) |
| `DATABASE_URL` | `postgres://postgres:postgres@127.0.0.1:5434/dbmp?sslmode=disable` | platform DB |
| `APP_ENCRYPTION_KEY` | kunci dev di Makefile | **wajib**; base64 dari 32 byte (`openssl rand -base64 32`) |
| `ADMIN_USERNAME` | `admin` | admin pertama |
| `ADMIN_PASSWORD` | (kosong) | wajib pada run pertama; min. 8 karakter; admin hanya dibuat jika belum ada admin aktif |
| `SESSION_IDLE_TIMEOUT` | `30m` | |
| `SESSION_ABSOLUTE_TIMEOUT` | `8h` | |
| `SESSION_ROTATE_INTERVAL` | `15m` | |
| `SESSION_GRACE_PERIOD` | `30s` | |
| `WORKER_CONCURRENCY` | `4` | jumlah slot worker (job paralel untuk database berbeda) |
| `WORKER_LEASE` | `30s` | lease job dan lock database; diperpanjang heartbeat tiap lease/3 |
| `WORKER_MAX_ATTEMPTS` | `5` | batas retry kegagalan sementara |
| `WORKER_DRAIN_TIMEOUT` | `30s` | saat SIGTERM/SIGINT, worker berhenti mengambil job baru dan menunggu job yang sedang berjalan selesai selama batas ini; bila terlewati, job dibatalkan dan dilanjutkan pemulihan |
| `LOG_LEVEL` | `info` | `debug` juga mencatat setiap query ke platform DB |

> Kunci enkripsi di Makefile hanya untuk development; untuk production buat kunci sendiri.
> `APP_ENV=production` mewajibkan HTTPS (mis. lewat reverse proxy). Tanpa HTTPS browser tidak
> menyimpan cookie session.

### Database sampel

`target-db` (`127.0.0.1:5435`, database `sample`, user/password `app`/`app`) berisi data contoh
untuk mencoba viewer dan migration (`internal/db/target-seed.sql`). `make reset-target`
mengembalikannya ke kondisi awal. User `app` adalah superuser; pada penggunaan nyata, pakai user
target dengan hak minimum.

`target-mariadb` (`127.0.0.1:3307`, database `sample`, user/password `app`/`app`) berisi data contoh
MariaDB (`internal/db/target-seed-mysql.sql`), termasuk tabel dengan foreign key, check constraint,
view, dan tabel tanpa primary key. Saat menambah koneksi, pilih engine `mariadb`.
`make reset-mariadb` mengembalikannya ke kondisi awal.

### Data uji besar

```bash
make seed-large LARGE_ROWS=2000000
```

Target ini membuat ulang tabel stabil `zz_big(id bigint PRIMARY KEY, x int, pad text)` di
`target-db`, mengisi `id` dari 1 sampai jumlah baris, lalu mengukur satu `UPDATE` seluruh baris
tanpa batch. Kolom batch yang dipakai Kei adalah `id`.

Bukti yang tercatat (PostgreSQL, diukur lewat `make seed-large`):

| Jumlah baris | Waktu isi data | Satu UPDATE penuh |
|---|---|---|
| 2.000.000 | 3.206 ms | 3.662 ms |

`UPDATE` tanpa batch memang lebih dari 1 detik pada ukuran ini, sehingga mode batch bisa diuji
dengan `batch_column=id` dan `batch_size=50000`. Setelah target ini dijalankan, tabel `zz_big`
sudah ada; test yang membuat ulang tabel itu harus menjatuhkannya terlebih dahulu atau memakai
tabel yang sudah diisi.

#### Mode biasa dan mode batch pada dataset yang sama (27 November 2026)

Dataset yang sama (`zz_big`, 2.000.000 baris; `make seed-large` kali ini mencatat isi data 3.473 ms
dan satu `UPDATE` penuh 5.546 ms) dijalankan lewat alur lengkap: buat migration, ajukan, setujui
leader, Jalankan, lalu dieksekusi worker.

| Mode | SQL up | Batch | Row committed | Durasi worker |
|---|---|---:|---:|---:|
| Biasa | `UPDATE zz_big SET x = x + 1` | - | 2.000.000 (satu transaction) | 4.776 ms |
| Per batch | `UPDATE zz_big SET x = x + 1 WHERE id IN (SELECT id FROM zz_big WHERE id > $1 ORDER BY id LIMIT $2) RETURNING id` | 40 × 50.000 | 2.000.000 | 24.903 ms |

SQL down mode batch: `UPDATE zz_big SET x = x - 1 WHERE id > $1 AND id <= $2`. Mode batch lebih lama
karena setiap batch adalah transaction tersendiri dengan checkpoint, tetapi tidak pernah mengunci
seluruh tabel sekaligus dan progresnya (batch dan row) terlihat di monitor job.

**Worker dimatikan di tengah batch.** Migration batch yang sama dijalankan lagi, lalu proses worker
dihentikan paksa (`kill -9`) setelah 13 batch (650.000 row) committed. Checkpoint di
`dbmp_meta.batch_checkpoint` saat itu berisi tepat 13 baris. Worker baru dijalankan:

1. Sekitar 15 detik kemudian lease lock habis; pemulihan menandai eksekusi `uncertain` dan
   menjadwalkannya ulang.
2. Worker mengklaim eksekusi yang sama (percobaan ke-2) dan melanjutkan dari batch 14.
3. Eksekusi selesai `success` dengan 40 batch dan 2.000.000 row.

Bukti tidak ada batch yang diulang atau terlewat: `sum(x)` naik tepat 2.000.000 (dari 4.000.000 ke
6.000.000) dan tabel checkpoint berisi 40 baris dengan `batch_no` 1 sampai 40 tanpa duplikat.

### Log SQL dan tracing

Server dan worker menulis log JSON (`log/slog`) ke stdout. Setiap query PostgreSQL dicatat oleh
`internal/obs/sqltrace` yang dipasang sebagai `pgx` tracer, sehingga tidak ada query yang terlewat:
query viewer yang read-only, `begin`, `commit`, `rollback`, batch, dan SQL migration.

| Pool | Level | Keterangan |
|---|---|---|
| Database target (koneksi) | `INFO` | selalu tampil; atribut `database` berisi id koneksi |
| Platform DB (sesi, grant, antrean) | `DEBUG` | tampil dengan `LOG_LEVEL=debug`; query yang gagal selalu tampil di `WARN` |

Atribut per query: `event=sql.query`, `db_target`, `query`, `params`, `duration_ms`, `rows`,
`status`, `actor` (id pengguna yang login, atau pengaju eksekusi di worker), dan untuk kegagalan
`sqlstate` serta `error`. Literal kredensial di SQL (`PASSWORD '...'`,
password di URL koneksi) disamarkan; parameter query yang menyentuh kolom password, token, sesi, atau
hash ditulis sebagai `***`, dan byte mentah tidak pernah dicatat.

Query yang berjalan di dalam request API atau job worker membuat span `db.query` dan log-nya membawa
`trace_id` serta `span_id`. Trace context ikut lewat antrean (kolom `traceparent` pada job), sehingga
satu migration membentuk satu trace: `POST /api/migrations/{id}/execute` → `migration.execute`
(worker) → `lock.acquire` → `migration.run` → `db.query`.

Contoh nyata dari pengujian k6 (satu trace `0b5dc5dd…`). Server menerima `execute` dan memasukkan job
ke antrean dengan span `bace4ffc…`:

```json
{"time":"2026-11-27T22:13:47.499+07:00","level":"INFO","msg":"request","method":"POST","path":"/api/migrations/1/execute","status":202,"trace_id":"0b5dc5ddba1734ca63dcd192694cb72d","span_id":"bace4ffc4ccff94d","actor":2}
{"time":"2026-11-27T22:13:47.499+07:00","level":"INFO","msg":"execution.enqueued","event":"execution.enqueued","database":1,"migration":1,"execution":1,"job":"2f9ec3dd-57a0-4d2b-b1d7-feba6e874602","trace_id":"0b5dc5ddba1734ca63dcd192694cb72d","span_id":"bace4ffc4ccff94d","actor":2}
```

Worker melanjutkan trace yang sama dari kolom `traceparent` job. Rantai span: `migration.execute`
(`6d45c102…`, induk `bace4ffc…` dari server) → `lock.acquire` (`4f369bb4…`) dan `migration.run`
(`e9079b5a…`) → `db.query` (`e49bbb7d…`). Log query dan span-nya:

```json
{"time":"2026-11-27T22:13:48.640+07:00","level":"INFO","msg":"sql.query","event":"sql.query","db_target":"target","query":"SELECT pg_sleep(1)","params":[],"duration_ms":1001.389,"database":1,"trace_id":"0b5dc5ddba1734ca63dcd192694cb72d","span_id":"e49bbb7dc965eac9","status":"ok","rows":1,"command":"SELECT 1","actor":2}
```

```json
{
  "Name": "db.query",
  "SpanContext": { "TraceID": "0b5dc5ddba1734ca63dcd192694cb72d", "SpanID": "e49bbb7dc965eac9" },
  "Parent": { "TraceID": "0b5dc5ddba1734ca63dcd192694cb72d", "SpanID": "e9079b5a665a5bc2" },
  "SpanKind": 3,
  "StartTime": "2026-11-27T22:13:47.638936939+07:00",
  "EndTime": "2026-11-27T22:13:48.640334342+07:00",
  "Attributes": [
    { "Key": "db.system", "Value": { "Type": "STRING", "Value": "postgresql" } },
    { "Key": "db.target", "Value": { "Type": "STRING", "Value": "target" } },
    { "Key": "db.connection_id", "Value": { "Type": "INT64", "Value": 1 } },
    { "Key": "db.statement", "Value": { "Type": "STRING", "Value": "SELECT pg_sleep(1)" } },
    { "Key": "db.rows", "Value": { "Type": "INT64", "Value": 1 } }
  ],
  "Status": { "Code": "Unset", "Description": "" }
}
```

Span di atas dipersingkat (field resource dan instrumentation scope dihilangkan). Span dicetak oleh
exporter console OpenTelemetry ke stdout; query yang gagal mendapat `Status.Code = Error` dengan
SQLSTATE sebagai deskripsi.

### Worker di Docker Compose

`make up` juga menjalankan satu worker (service `worker`). Worker ini memakai `network_mode: host`
agar alamat koneksi target yang disimpan di platform (mis. `127.0.0.1:5435`) sama dengan yang dipakai
server. Satu proses worker menjalankan `WORKER_CONCURRENCY` slot (default 4), sehingga migration pada
database yang berbeda berjalan paralel. Satu database tetap hanya menjalankan satu migration: lock per
database dan klaim eksekusi berdasarkan status berlaku lintas slot maupun lintas proses, jadi worker
tambahan (`make run-worker`) aman dijalankan bersamaan. Jangan memakai `docker compose up --scale
worker=N` karena `WORKER_ID` di compose sama untuk semua replika. Saat dihentikan (`docker compose
stop`, Ctrl+C, atau SIGTERM), worker menyelesaikan job yang sedang berjalan lebih dulu, dibatasi
`WORKER_DRAIN_TIMEOUT`. Diuji: SIGTERM dikirim saat batch ke-10 dari 40; worker tidak mengambil job
baru, menyelesaikan ke-40 batch pada percobaan yang sama, lalu keluar dengan kode 0. Image worker dibangun dari kode saat
itu: setelah mengubah kode jalankan `docker compose build worker`.

### Stress testing k6 (bonus Vara)

Script `scripts/k6/stress.js` menguji API viewer dan alur submission job secara bersamaan. Jalankan
server dan worker terlebih dahulu (`make run` dan `make run-worker`, dengan Docker sudah aktif untuk
database target). Siapkan dua koneksi database yang sama-sama punya akses **View + Edit** untuk
pengaju dan leader, serta minimal satu tabel pada masing-masing database. Pengaju dan leader harus
berbeda karena pengaju tidak boleh menyetujui permintaannya sendiri.

```bash
k6 run \
  -e BASE_URL=http://localhost:8080 \
  -e K6_USER=pengaju \
  -e K6_LEADER=leader \
  -e K6_PASSWORD='password-bersama' \
  -e K6_SUMMARY_JSON=summary.json \
  scripts/k6/stress.js
```

Tanpa k6 terpasang, jalankan lewat Docker (`--network host` agar `localhost` menunjuk ke server):

```bash
docker run --rm --network host -v "$PWD/scripts/k6:/scripts:ro" grafana/k6:0.54.0 run \
  -e BASE_URL=http://localhost:8080 -e K6_USER=pengaju -e K6_LEADER=leader \
  -e K6_PASSWORD='password-bersama' /scripts/stress.js
```

Variabel opsional: `K6_DB_A` dan `K6_DB_B` untuk memilih dua database secara eksplisit,
`K6_USER_PASSWORD` dan `K6_LEADER_PASSWORD` bila password berbeda, `K6_STAGE_SECONDS` (default `30`)
untuk durasi tiap level beban, `K6_JOB_TIMEOUT_SECONDS` (default `20`) untuk batas polling job, dan
`K6_JOB_SQL` (default `SELECT pg_sleep(1)`) untuk SQL up migration uji.
Tanpa `K6_DB_A`/`K6_DB_B`, script memilih dua database edit pertama yang terlihat oleh pengaju.

`setup()` login sebagai pengaju dan leader, mengambil daftar database serta satu tabel dari tiap
database, lalu membuat sembilan migration yang tidak mengubah data (`SELECT pg_sleep(1)`, down
`SELECT 1`), mengajukannya, dan menyetujuinya. Migration sengaja berjalan sekitar satu detik agar
waktu tunggu antrean dan durasi job terukur.
Indeks migration disimpan sebagai slot `SharedArray`; tiap job memakai kombinasi offset stage dan
`exec.scenario.iterationInTest` sehingga tidak ada migration yang dipakai dua kali. SQL, user id,
job id, dan URL tidak dipakai sebagai tag metrik; tag yang dipakai hanya `stage`, `op`, `db`,
`status`, dan `name`.

Skenario memakai `constant-vus` untuk pembacaan API dan submission job pada enam level beban:
baseline (5 API + 1 job), ramp-1 (10 + 1), ramp-2 (20 + 2), peak (30 + 3), cool-down (10 + 1), dan
recovery (5 + 1). Pembacaan bergiliran di database A dan B serta tiga endpoint (`/api/databases`,
detail database, dan halaman tabel). Submission job juga bergiliran di database A dan B, sehingga
beban API dan job menguji database yang sama maupun berbeda. Setiap `POST /execute` harus balas
`202` dengan `job_id`; status `202` hanya dihitung sebagai job diterima, bukan sebagai eksekusi
berhasil. Job dipolling tiap 500 ms sampai status terminal (`success`, `failed`, `partial`,
`uncertain`, `partially_reverted`, atau `cancelled`) atau timeout. Hanya status `success` yang
menaikkan `jobs_success`.

Metrik yang dicatat: p50/p95 `http_req_duration` dengan `op=api-read`, throughput `http_reqs`,
`http_req_failed`, `job_queue_wait_ms`, `job_duration_ms`, `jobs_completed`, throughput job
(dari `jobs_completed`), dan `jobs_success`. Waktu tunggu antrean dihitung dari `queued_at` sampai
`started_at` pada respons `GET /api/jobs/{id}` (presisi milidetik), durasi job dari `started_at`
sampai `finished_at`. Ringkasan per stage tampil di stdout; bila
`K6_SUMMARY_JSON` diisi, file tersebut berisi tabel yang sama plus data ringkasan mentah k6.

Threshold (dipilih dengan margin besar di atas baseline; seluruhnya lulus pada hasil di bawah):

| Metrik | Threshold | Tujuan |
|---|---|---|
| `http_req_duration` | `p(95)<800` | batas latensi seluruh request |
| `http_req_duration{op:api-read}` | `p(95)<500` | batas p95 pembacaan API |
| `http_req_failed` | `rate<0.01` | batas error HTTP global |
| `http_req_failed{op:api-read}` | `rate<0.01` | batas error pembacaan API |
| `job_submit_accepted` | `rate>0.95` | sebagian besar submit mendapat `202` |
| `job_queue_wait_ms` | `p(95)<5000` | antrean tidak menumpuk |
| `jobs_success` | `rate>0.95` | sebagian besar job mencapai `success` |

Script juga membuat sub-metrik per stage untuk p95 API, error rate, antrean, durasi job,
throughput job, dan tingkat sukses, supaya tabel hasil bisa diisi langsung dari ringkasan.

#### Hasil (27 November 2026)

Lingkungan: satu mesin pengembangan (16 CPU), server dan worker dari `go build` dijalankan lokal
(worker `WORKER_CONCURRENCY=4`), platform DB dan database target dari `docker compose`, k6 0.54.0 lewat
Docker. Database A adalah `sample` dan database B adalah database kedua di server `target-db` yang
berisi tabel 100 baris. Durasi tiap stage 30 detik. Semua threshold lulus (`exit 0`) dan seluruh 9
job selesai `success`.

| Stage | API VU | Job VU | p50 API (ms) | p95 API (ms) | API (req/s) | Error API | Queue p95 (ms) | Durasi job rata-rata (ms) | Job (/s) | Job sukses |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| baseline | 5 | 1 | 7.94 | 15.83 | 19.33 | 0 | 11 | 1015 | 0.03 | 100% |
| ramp-1 | 10 | 1 | 7.43 | 19.05 | 38.60 | 0 | 18 | 1017 | 0.03 | 100% |
| ramp-2 | 20 | 2 | 7.14 | 19.58 | 77.33 | 0 | 248 | 1014 | 0.07 | 100% |
| peak | 30 | 3 | 7.33 | 15.75 | 116.03 | 0 | 1171 | 2093 | 0.10 | 100% |
| cool-down | 10 | 1 | 7.08 | 20.32 | 38.67 | 0 | 26 | 1008 | 0.03 | 100% |
| recovery | 5 | 1 | 8.12 | 18.52 | 19.33 | 0 | 20 | 1008 | 0.03 | 100% |

Analisis:

- **baseline**: p95 API sekitar 16 ms, jauh di bawah threshold 500 ms. Waktu tunggu antrean
  ~10 ms karena worker langsung mengambil job; durasi job ~1 detik sesuai `pg_sleep(1)`.
- **ramp-1 dan ramp-2**: throughput API naik linear dengan jumlah VU (19 → 39 → 77 req/s) dan p95
  tetap di bawah 20 ms. Dua job di ramp-2 berada di database A dan B, sehingga berjalan paralel pada
  slot worker berbeda; waktu tunggu p95 ~250 ms berasal dari interval polling worker.
- **peak**: API tetap stabil (116 req/s, p95 16 ms, tanpa error), jadi endpoint pembacaan bukan
  bottleneck. Kenaikan terjadi pada antrean: dua dari tiga job menuju database A, sedangkan satu
  database hanya boleh menjalankan satu migration. Log worker menunjukkan migration yang urutannya
  lebih akhir sempat diklaim lebih dulu; worker menahannya (`execution.waiting`, dicoba lagi setelah
  3 detik) sementara migration sebelumnya yang sempat mendapati database sibuk dicoba lagi setelah
  1 detik, berjalan, lalu disusul migration berikutnya. Hasilnya queue p95 1,2 detik dan durasi
  rata-rata 2,1 detik (durasi dihitung dari klaim pertama); semua job `success`, urutan linear
  terjaga, dan job di database B tidak ikut menunggu.
- **cool-down dan recovery**: semua metrik kembali ke nilai baseline (p95 API <21 ms, antrean
  <30 ms, durasi ~1 detik), tanpa error dan tanpa job tertinggal. Sistem pulih segera setelah beban
  turun.

Catatan: pengukuran sebelumnya dengan jeda tetap 5 detik untuk database sibuk menghasilkan queue p95
4,8 detik pada stage peak, karena migration sebelumnya dan sesudahnya saling menunda dengan jeda
yang sama. Jeda kini dibedakan (1 detik bila database sibuk, 3 detik bila belum giliran) sehingga
migration yang lebih awal selalu menang pada percobaan berikutnya.

### Security: SAST dengan CodeQL (bonus)

Workflow `.github/workflows/codeql.yml` menjalankan CodeQL untuk Go, JavaScript/TypeScript, dan
GitHub Actions pada setiap push ke `main`, `main-milestone-2`, dan `feat/**`, pada pull request, dan
terjadwal setiap Senin. Hasil scan terakhir (27 November 2026, `main-milestone-2` commit `d15d707` dan
`feat/revision-migration` commit `7b7f31e`): Go 5 hasil (35 rule), JavaScript/TypeScript 0 hasil
(103 rule), Actions 0 hasil (23 rule). Kelima hasil Go adalah alert yang sudah di-dismiss di bawah.

Seluruh temuan (13 alert, semuanya rule `go/log-injection`, severity medium):

| Alert | Lokasi | Potensi dampak | Keputusan | Alasan |
|---|---|---|---|---|
| #4, #6–#12 | `adminapi.go:234`, `app/app.go:139`, `app/errors.go:43`, `approvalapi.go:275`, `connectionapi.go:106`, `spaapi.go:147`, `sessionapi.go:150`, `viewerapi.go:118` | path request ditulis ke log; karakter baris baru di path dapat memalsukan baris log | **diperbaiki** (commit `7e8d187`) | path di-log lewat `httpapi.LogPath`, yang menghapus `\n` dan `\r`; diuji di `httpapi_test.go`. Alert tidak muncul lagi pada scan berikutnya |
| #1, #2 | `adminapi/users.go:152` | nilai dari body request masuk log | **false positive** | nilai `is_admin`/`is_active` bertipe bool hasil decode JSON, tidak dapat berisi baris baru; log ditulis `slog` JSON yang meng-escape string |
| #3, #5 | `adminapi/groups.go:444`, `:364` | id dari body request masuk log | **false positive** | nilai bertipe `int64` hasil decode JSON |
| #13 | `explainapi/explainapi.go:119` | flag `analyze` dari body request masuk log | **false positive** | nilai bool hasil decode JSON |

Catatan: log aplikasi seluruhnya memakai handler JSON `slog`, sehingga karakter kontrol pada string
selalu di-escape dan tidak dapat membuat baris log baru. Scan bersih atau temuan yang di-dismiss tidak
berarti aplikasi pasti aman; CodeQL hanya mencakup pola yang dikenali rule-nya. Branch
`feat/wp8-vara-fixes` menambahkan log SQL (`internal/obs/sqltrace`); scan untuk branch tersebut
berjalan otomatis setelah push dan temuannya perlu ditinjau dengan cara yang sama.

## Cara pakai singkat

1. **Admin**: Koneksi -> tambah koneksi (pilih engine) -> Grup -> buat grup, anggota, dan leader ->
   atur akses database grup. Admin baru melihat database di halaman Database dan Migration bila ia
   sendiri anggota grup yang punya akses.
2. **Pengaju (View + Edit)**: Migration -> pilih database -> buat migration (SQL up dan down, atau
   irreversible dengan alasan; mode biasa atau per batch) -> ajukan review.
3. **Leader setiap grup yang punya akses**: Persetujuan -> setujui atau tolak revisi tersebut.
4. **Pengaju**: setelah Approved, tekan Jalankan. Halaman monitor job menampilkan status, batch dan
   row yang sudah committed, serta tombol retry, resume, cancel, rollback, atau forward yang diizinkan.
   Rollback dan forward juga melewati review dan approval.
5. **Riwayat**: Migration -> Riwayat; centang Hide rollback untuk menyembunyikan pasangan eksekusi
   yang sudah di-rollback.
6. **Schema diff (View + Edit)**: Migration -> Schema diff -> ubah DDL kondisi yang diinginkan ->
   Bandingkan -> edit SQL bila perlu -> simpan sebagai draft migration.

Server (`make run`) dan worker (`make run-worker` atau service `worker` dari `make up`) harus
berjalan; tanpa worker, job tetap berstatus Pending (Queue).

## Keputusan desain

### Session

| Parameter | Nilai | Keterangan |
|---|---|---|
| Idle timeout | 30 menit | berakhir bila tidak dipakai |
| Absolute expiration | 8 jam | berakhir meski masih aktif; rotasi tidak memperpanjangnya |
| Rotasi berkala | 15 menit | nilai cookie diganti, nama tetap |
| Grace period | 30 detik | identifier lama masih berlaku sebentar untuk request yang berjalan bersamaan |

- Session id acak 32 byte dari `crypto/rand`; hanya hash-nya yang disimpan di database.
- Rotasi terjadi saat login, saat privilege naik (menjadi admin, masuk grup, atau akses grup naik), dan berkala.
- Logout, user dinonaktifkan, password direset, user dikeluarkan dari grup, akses grup turun atau
  dicabut, dan koneksi dihapus mengakhiri session di backend. Mengirim ulang form tanpa perubahan tidak berefek.

### Akses

- Akses ke sebuah database diberikan lewat grup, bukan langsung ke user.
- Jika user ada di beberapa grup yang mengakses database yang sama, **level tertinggi yang berlaku**
  (View + Edit mengalahkan View Only).
- Platform Admin bukan level akses database: admin tanpa grant mendapat 403 pada halaman database.
- Urutan pemeriksaan pada halaman database: format id salah → 404; tidak punya akses (termasuk id
  yang tidak ada) → 403; baru kemudian koneksi tidak ada → 404. Id yang ada tidak dapat ditebak.

### Migration

| Keputusan | Pilihan |
|---|---|
| Alur status | Draft → Pending Review → Approved → Pending (Queue) → Running → Success / Failed. Rollback dan forward juga melewati review dan approval. |
| Urutan linear | Nomor urut per database diberikan saat migration pertama kali diajukan untuk review. Migration yang ditolak atau dibatalkan sebelum pernah berjalan melepas nomornya; yang gagal tetap memegangnya. Posisi aktif = eksekusi sukses terakhir yang belum di-rollback. |
| Revisi | SQL up dan down (atau irreversible dengan alasan wajib) disimpan per revisi beserta checksum. Tabel revisi append-only (trigger). Mengubah SQL membuat revisi baru dan persetujuan lama tidak berlaku. |
| Eksekusi | Selalu asynchronous: API mengembalikan `202` dan `job_id`, worker (`cmd/worker`) mengambil job dari antrean PostgreSQL (`FOR UPDATE SKIP LOCKED`, lease, dan heartbeat). Mode biasa memakai satu transaction; mode batch memakai satu transaction per batch dengan checkpoint di `dbmp_meta` pada database target, ditulis dalam transaction yang sama. |
| Rollback dan forward | Rollback hanya untuk migration aktif terakhir yang reversible, memakai SQL down asli revisi yang dieksekusi, dan merujuk execution ID yang dibatalkannya. Forward menjalankan ulang SQL up revisi yang sama dan tidak menjalankan migration berikutnya. |
| Migration history | Mencatat semua attempt, berhasil maupun gagal (spesifikasi v4): migration, execution ID, revisi, pengguna, waktu, status, attempt, dan salinan SQL yang dijalankan. Tabel append-only. Hanya baris sukses yang mengubah posisi aktif. Hide rollback menyembunyikan pasangan sukses (eksekusi dan rollback-nya) berdasarkan execution ID; attempt gagal selalu tampil. |
| Migration gagal | Tetap memegang nomor urutnya dan tercatat di history. Boleh dijalankan lagi atau dilewati oleh migration berikutnya. |
| Statement timeout | 15 menit per transaction (`SET LOCAL statement_timeout`), menunggu lock tabel maksimal 10 detik (`lock_timeout`). |
| Konkurensi | Satu database hanya menjalankan satu migration (baris lock per database di platform DB dengan lease dan heartbeat, ditambah unique index status sibuk). Database berbeda dapat berjalan paralel di worker berbeda. Status `partial` dan `uncertain` menahan migration berikutnya pada database yang sama. |
| Worker mati | Lease lock habis → eksekusi `running` ditandai `uncertain` dan dijadwalkan ulang. Worker memeriksa marker di `dbmp_meta.execution_marker` (ditulis dalam transaction migration) sebelum mengulang: ada marker berarti sudah commit, sehingga tidak terjadi commit ganda. Mode batch melanjutkan dari checkpoint terakhir. |
| Retry | Kegagalan sementara diulang dengan jeda bertingkat dan batas attempt (`WORKER_MAX_ATTEMPTS`). Pesan antrean duplikat tidak menjalankan eksekusi dua kali karena eksekusi diklaim berdasarkan statusnya. |
| Peninjauan | Approval terikat pada revisi; worker memvalidasi ulang checksum, persetujuan, hak akses pengaju, dan posisi aktif sebelum menjalankan SQL. |
| Validasi | Judul wajib (maks 100 karakter), SQL wajib (maks 100 KB). `BEGIN`, `COMMIT`, `ROLLBACK`, `SAVEPOINT`, `START TRANSACTION` ditolak karena transaction dikelola platform. |
| Hak akses | Lihat riwayat dan detail: View Only. Buat, edit, tinjau, jalankan: View + Edit. Dicek di backend pada setiap request. |
| Hapus koneksi | Diblokir bila punya riwayat migration (foreign key `RESTRICT`, HTTP 409). |
| Logging | Log JSON untuk setiap attempt dan setiap query SQL, dengan trace_id/span_id (lihat "Log SQL dan tracing"). Password, kredensial di SQL, dan session id disamarkan. |
| EXPLAIN / EXPLAIN ANALYZE | Hanya untuk draft dan butuh View + Edit. Dijalankan per statement dalam satu transaction yang selalu di-rollback, dengan timeout yang sama. Statement DDL dijalankan sementara agar statement berikutnya bisa dianalisis. |

### Schema diff

| Keputusan | Pilihan |
|---|---|
| Kondisi pembanding | DDL kondisi yang diinginkan di textarea, awalnya diisi dari `schemadiff.Dump` schema saat ini. Tidak membuka koneksi kedua; satu schema dibandingkan per request. |
| Eksekusi DDL | Dijalankan dalam transaction pada schema sementara `zz_schemadiff_<hex>` dengan `search_path` schema target, `public`, lalu `pg_catalog`. Transaction selalu di-rollback dan schema sementara ikut hilang, sehingga database target tidak berubah. |
| Batas waktu | 25 detik per preview, dengan `statement_timeout` 20 detik dan `lock_timeout` 5 detik pada koneksi target. |
| Validasi | Aturan yang sama dengan migration: maksimal 100 KB; `BEGIN`, `COMMIT`, `ROLLBACK`, `SAVEPOINT`, dan `START TRANSACTION` ditolak. Pesan error DDL ditampilkan tanpa host, port, atau kredensial. |
| Cakupan | Tabel biasa, kolom, constraint, index, view, enum, dan sequence. Nama objek di-qualify dengan schema target. SQL hasil diawali `SET LOCAL search_path` agar foreign key atau view yang merujuk objek di `public` tetap dapat diselesaikan. |
| Perubahan destruktif | `DROP TABLE`, `DROP COLUMN`, `DROP TYPE`, dan `DROP SEQUENCE` ditulis sebagai komentar secara default. Checkbox "Sertakan perubahan destruktif" mengaktifkannya dan menambahkan peringatan. |
| Hasil | Daftar perubahan per objek, peringatan (mis. perubahan tipe, `NOT NULL` tanpa default, enum yang tidak dapat diubah langsung), dan SQL yang masih dapat diedit. |
| Menyimpan | SQL dikirim ke fitur migration yang sudah ada, sehingga menjadi draft biasa: judul dan SQL dapat diubah, ditinjau dengan checksum, lalu dijalankan dalam transaction. |
| Hak akses | View + Edit untuk membuka halaman, preview, dan menyimpan draft. View Only mendapat 403 dan tombolnya disembunyikan. |
| Logging | Hanya id user, database, dan migration; DDL/SQL, password, dan session id tidak dicatat. |

### MySQL/MariaDB (bonus dukungan DBMS tambahan)

| Keputusan | Pilihan |
|---|---|
| Engine | `postgres`, `mysql`, atau `mariadb`, disimpan di kolom `engine` tabel `db_connections` (default `postgres`, sehingga koneksi lama tetap valid). Dipilih saat koneksi dibuat dan tidak dapat diubah, agar riwayat migration tidak menjadi tidak konsisten. |
| Pemilihan jalur | `dbconn.Manager` menyediakan `Get` (pool pgx, PostgreSQL) dan `GetSQL` (`database/sql`, MySQL/MariaDB). Memanggil yang tidak sesuai dengan engine koneksi mengembalikan `ErrWrongEngine`. Access control, session, grant, dan riwayat migration tetap di platform DB PostgreSQL dan tidak berubah. |
| Cakupan | Satu koneksi MySQL/MariaDB = satu database (schema = database). Introspeksi dibatasi ke database koneksi lewat `information_schema`. |
| SSL mode | Pilihan form dipetakan: `disable` = tanpa TLS; `allow` dan `prefer` = TLS bila tersedia; `require` = TLS tanpa verifikasi sertifikat; `verify-ca` dan `verify-full` = TLS terverifikasi. |
| Eksekusi migration | SQL dipecah per statement (menghormati string, identifier ber-backtick, dan komentar `--` dan `/* */`) dan dijalankan dalam satu transaction. Bila ada statement gagal, pesan error menyebut nomor statement dan memperingatkan kemungkinan perubahan parsial. |
| Timeout | Seluruh migration dibatasi 30 detik lewat context, ditambah `lock_wait_timeout` 10 detik per sesi. Viewer dibatasi 5 detik. |
| Viewer | Transaction read-only; nilai kolom diambil sebagai teks dan dipotong 500 karakter seperti di PostgreSQL. Tabel tanpa primary key tidak punya urutan baris yang stabil karena MySQL tidak memiliki padanan `ctid`. |
| Klasifikasi error | Kode error MySQL (1044/1045 autentikasi, 1049 database tidak ada, 1040/1203 koneksi ditolak) dipetakan ke kategori yang sama dengan PostgreSQL, sehingga halaman error tidak membocorkan host, port, atau kredensial. |
| EXPLAIN dan schema diff | Belum didukung: tombol disembunyikan di halaman review koneksi MySQL/MariaDB. |

### Koneksi dan viewer

- Password koneksi dienkripsi AES-256-GCM dengan kunci dari `APP_ENCRYPTION_KEY`; server menolak start
  bila kunci tidak valid. Password tidak pernah ditampilkan kembali.
- Viewer read-only: transaksi read-only dengan statement timeout 5 detik, nama schema dan tabel
  di-quote serta dibatasi whitelist hasil introspeksi, paginasi 25 baris (maks 100), sel dipotong pada 500 karakter.

## Keterbatasan yang diketahui

- User dengan akses View + Edit dipercaya: migration berjalan dengan hak penuh user koneksi target dan
  dapat mengubah `statement_timeout` dari dalam SQL-nya. Untuk penggunaan nyata pakai user target dengan hak minimum.
- "Test connection" membuat server menyambung ke host yang diisi admin (hanya admin).
- Tidak ada pembatasan percobaan login (rate limit); bcrypt memperlambat tebakan tetapi tidak memblokirnya.
- `sslmode` bawaan form adalah `prefer`, yang boleh jatuh ke koneksi tanpa enkripsi. Untuk server jauh pakai `require` atau `verify-full`.
- Perintah yang tidak boleh berjalan dalam transaction (mis. `CREATE INDEX CONCURRENTLY`, `VACUUM`) akan gagal dan di-rollback.
- Rollback hanya membatalkan perubahan transaksional; nilai sequence yang sudah terpakai tidak dikembalikan.
- Schema diff hanya mengenali tabel biasa, kolom, constraint, index, view, enum, dan sequence. Partitioned
  table, materialized view, foreign table, trigger, function/procedure, extension, comment, owner,
  privilege/grant, serta RLS/policy tidak didiff sehingga perubahannya tidak terdeteksi dan tidak dibuatkan SQL.
- **MySQL/MariaDB: DDL tidak dapat di-rollback.** `CREATE`, `ALTER`, `DROP`, dan `TRUNCATE` otomatis
  di-commit oleh server, dan statement sesudahnya berjalan tanpa transaction. Migration yang gagal di
  tengah dapat meninggalkan perubahan parsial: status migration tetap `failed` dan pesan error menyebut
  statement yang gagal, tetapi statement DDL sebelumnya sudah berlaku. Rollback penuh hanya berlaku untuk
  migration yang seluruhnya berisi DML. Halaman review menampilkan peringatan ini.
- MySQL/MariaDB: pemecah statement belum menangani komentar `#`, backslash-escape di dalam string literal,
  dan `DELIMITER` atau stored procedure; gunakan statement sederhana yang dipisah `;`.
- MySQL/MariaDB: analisis execution plan (EXPLAIN) dan schema diff belum didukung.
- MySQL/MariaDB: tabel tanpa primary key tampil tanpa urutan baris yang stabil, sehingga isi tiap halaman
  bisa berubah antar permintaan. Introspeksi mencakup tabel dan view pada database koneksi saja.

## Test

```bash
make test-unit          # tanpa database (-short)
make test-integration   # butuh `make up`; gagal (bukan skip) bila database tidak ada
make test               # keduanya
make test-integration PKG=./internal/migration   # satu paket
```

Jalankan lewat Makefile, bukan `go test` langsung, karena variabel `TEST_DATABASE_URL`,
`TARGET_DB_URL`, dan `TARGET_MARIADB_ADDR` diisi di sana. Test migration membuat tabel `zz_mig_*` di
database sampel dan menghapusnya lagi; bila ada sisa, jalankan `make reset-target`. Test MariaDB memakai
`TARGET_MARIADB_ADDR` (default Makefile `127.0.0.1:3307`) dan membuat tabel `zz_mig_my*`; bila ada sisa,
jalankan `make reset-mariadb`.

## Struktur

```
cmd/server            titik masuk
internal/app          handler HTTP dan routing
internal/auth         login, session, CSRF
internal/authz        kebijakan akses (satu-satunya tempat keputusan izin)
internal/users        user dan grup
internal/dbconn       koneksi target (PostgreSQL dan MySQL/MariaDB), enkripsi password, grant
internal/explorer     introspeksi schema dan viewer data (PostgreSQL dan MySQL/MariaDB)
internal/migration    migration: repository, service, executor, sesi eksekusi per engine
internal/schemadiff   diff schema dan generator SQL migration
internal/platformdb   koneksi dan migrasi schema platform
internal/queue        antrean job PostgreSQL (dipakai server dan worker)
internal/obs          logger JSON, OpenTelemetry, pembungkus antrean; obs/sqltrace mencatat setiap query
internal/approval     persetujuan leader per grup
internal/migration/workflow  model, status, posisi aktif, history (Milestone 2)
internal/migration/exec      handler worker: lock, eksekusi normal dan batch, pemulihan
cmd/worker            proses worker antrean
web                   SPA (Vite, React, TypeScript, Tailwind), di-embed ke binary server
internal/testutil     helper test
internal/db           seed database sampel (PostgreSQL dan MariaDB)
```

## Pemecahan masalah

| Gejala | Penyebab / solusi |
|---|---|
| `belum ada admin, isi ADMIN_PASSWORD` | Run pertama butuh `ADMIN_PASSWORD` (min. 8 karakter). |
| `APP_ENCRYPTION_KEY tidak valid` | Buat kunci dengan `openssl rand -base64 32`. |
| `address already in use` atau `password authentication failed` pada port 5433, 5434, atau 5435 | Port dipakai PostgreSQL lain di mesin Anda. Hentikan proses itu, atau pakai database lain untuk test: `make test TEST_DATABASE_URL=postgres://...`. |
| `address already in use` pada port 3307 | Port dipakai MySQL/MariaDB lain. Hentikan proses itu, atau ubah port di `docker-compose.yml` dan `TARGET_MARIADB_ADDR`. |
| Test viewer gagal setelah test migration | `make reset-target` |
| Test MariaDB gagal setelah test migration MySQL | `make reset-mariadb` |
| Docker `permission denied` | Jalankan dengan `sudo`. |

## Tim

| Anggota | NIM | Pembagian Tugas |
|---|---|---|
| Helena Kristela Sarhawa | 13524109 | Access Control, Autentikasi, Sesi, Otorisasi, Manajemen User & Grup, Bonus EXPLAIN / EXPLAIN ANALYZE |
| Vara Azzara Ramli Pulukadang | 13524091 | Database Management, Encrypted Connection, Metadata Explorer, Data Viewer |
| Keisha Rizka Syofyani | 13524073 | Migration Engine, Transaksi & Locking, History, Dokumentasi, README, Bonus Dukungan DBMS MySQL/MariaDB |