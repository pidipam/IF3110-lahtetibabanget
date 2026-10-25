# IF3110-lahtetibabanget
Tugas Besar IF3110 Pengembangan Aplikasi Web 2026

# Database Management Platform

Platform web untuk mengelola akses dan perubahan schema database bersama (PostgreSQL, dengan
dukungan tambahan MySQL/MariaDB): akses diatur per grup, struktur dan data dapat dijelajahi, dan
setiap perubahan dilakukan lewat migration yang tercatat. Tugas Besar IF3110 Pengembangan Aplikasi
Web 2026, Milestone 1.

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
- **Migration**: editor SQL, review sebelum eksekusi, eksekusi dalam transaction (commit/rollback),
  versi linear per database, lock anti-paralel, statement timeout, dan riwayat.
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

Go (backend), PostgreSQL 16 (platform DB dan target), MariaDB 11 (target tambahan), `html/template`
(bukan SPA), pgx, go-sql-driver/mysql, Docker Compose.

## Prasyarat

Go (versi sesuai `go.mod`), Docker + Docker Compose, `make`, dan `openssl`.
Di beberapa mesin Docker membutuhkan `sudo`.

## Menjalankan

```bash
make up                                    # platform-db :5434, test-db :5433, target-db (sampel PG) :5435, target-mariadb (sampel) :3307
ADMIN_PASSWORD='ganti-password' make run   # run pertama membuat akun admin
```

Buka http://localhost:8080 dan login sebagai `admin` dengan password tadi. Schema platform
dibuat otomatis saat server mulai. `make down` mematikan container; data tetap tersimpan di
volume. Untuk menghapus semua data (termasuk akun admin) jalankan `docker compose down -v`.

`make run` membaca file `.env` bila ada (lihat `.env.example`; jangan di-commit).

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

## Cara pakai singkat

1. **Admin**: Koneksi -> tambah koneksi (pilih engine) -> Grup -> buat grup dan anggota -> atur akses database grup.
2. **User**: Database -> pilih database -> lihat struktur dan data -> Migration -> buat draft, tinjau, jalankan.
3. **Schema diff (View + Edit)**: Database -> Schema diff -> pilih schema -> ubah DDL kondisi yang
   diinginkan -> Bandingkan -> periksa perbedaan dan SQL -> edit bila perlu -> simpan sebagai draft
   migration. Draft tersebut kemudian ditinjau dan dijalankan lewat halaman Migration.

Halaman migration: `/databases/{id}/migrations` (tombol "Migration" di halaman database).
Halaman schema diff: `/databases/{id}/schema-diff` (tombol "Schema diff" di halaman database,
hanya untuk View + Edit).

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
| Pemberian versi | Saat eksekusi dimulai, di bawah lock: `MAX(version)+1` per database. Draft belum punya versi, sehingga versi linear tanpa celah. |
| Migration gagal | Tetap memakai nomor versinya dan tampil di riwayat sebagai gagal. Untuk mencoba ulang, buat draft baru. |
| Yang boleh diedit | Hanya draft. Migration yang sudah dijalankan tidak berubah. |
| Eksekusi | Satu transaction di database target: commit bila seluruh SQL berhasil, rollback bila ada yang gagal. Pada MySQL/MariaDB, DDL tidak dapat di-rollback (lihat bagian MySQL/MariaDB). |
| Statement timeout | 30 detik (`SET LOCAL statement_timeout`); menunggu lock tabel maksimal 10 detik (`lock_timeout`). Nilai ada di kode, tidak diatur lewat environment. |
| Eksekusi paralel | Advisory lock per database di platform DB. Jika sudah dipegang, eksekusi kedua langsung ditolak (HTTP 409) dan migration tetap draft. |
| Target tidak dapat dihubungi | Dicek sebelum nomor versi dipakai; migration tetap draft dan user melihat halaman 503 tanpa host/port. |
| Proses mati saat eksekusi | Saat startup, migration berstatus "running" diubah menjadi gagal. |
| Peninjauan | Halaman review membawa checksum SQL; bila SQL berubah sebelum dijalankan, eksekusi ditolak (409). |
| Validasi | Judul wajib (maks 100 karakter), SQL wajib (maks 100 KB). `BEGIN`, `COMMIT`, `ROLLBACK`, `SAVEPOINT`, `START TRANSACTION` ditolak karena transaction dikelola platform. |
| Hak akses | Lihat riwayat dan detail: View Only. Buat, edit, tinjau, jalankan: View + Edit. Dicek di backend pada setiap request. |
| Hapus koneksi | Diblokir bila punya riwayat migration (foreign key `RESTRICT`, HTTP 409). |
| Logging | Hanya id (user, database, migration) dan status. SQL, password, dan session id tidak dicatat. |
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
- Pemulihan migration "running" saat startup mengasumsikan satu instance aplikasi.
- Eksekusi berjalan sinkron di request HTTP tanpa indikator progres. Jika browser terputus, migration dibatalkan dan tercatat gagal.
- Perintah yang tidak boleh berjalan dalam transaction (mis. `CREATE INDEX CONCURRENTLY`, `VACUUM`) akan gagal dan di-rollback.
- Rollback hanya membatalkan perubahan transaksional; nilai sequence yang sudah terpakai tidak dikembalikan.
  Migration yang sudah di-commit tidak dapat dibatalkan; buat migration baru untuk membalikkannya.
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
internal/web          renderer dan template
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