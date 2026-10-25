# IF3110-lahtetibabanget
Tugas Besar IF3110 Pengembangan Aplikasi Web 2026

# Database Management Platform

Platform web untuk mengelola akses dan perubahan schema PostgreSQL bersama: akses diatur per
grup, struktur dan data dapat dijelajahi, dan setiap perubahan dilakukan lewat migration yang
tercatat. Tugas Besar IF3110 Pengembangan Aplikasi Web 2026, Milestone 1.

## Fitur

- **Autentikasi dan sesi**: login username/password (bcrypt), session acak yang disimpan sebagai hash,
  cookie HttpOnly + SameSite (+ Secure di production), idle timeout, absolute expiration, rotasi
  berkala dengan grace period, dan perlindungan CSRF.
- **Otorisasi**: user → grup → akses database (*View Only* / *View + Edit*), dicek di backend pada
  setiap request. Platform Admin mengelola user, grup, koneksi, dan akses, tetapi bukan level akses database.
- **Koneksi database**: CRUD koneksi PostgreSQL oleh admin, password dienkripsi (AES-256-GCM),
  uji koneksi, dan kegagalan koneksi ditangani dengan halaman error yang aman.
- **Database viewer**: schema, tabel, kolom, constraint, index, dan data (read-only, paginasi).
- **Migration**: editor SQL, review sebelum eksekusi, eksekusi dalam transaction (commit/rollback),
  versi linear per database, lock anti-paralel, statement timeout, dan riwayat.

## Teknologi

Go (backend), PostgreSQL 16, `html/template` (bukan SPA), pgx, Docker Compose.

## Prasyarat

Go (versi sesuai `go.mod`), Docker + Docker Compose, `make`, dan `openssl`.
Di beberapa mesin Docker membutuhkan `sudo`.

## Menjalankan

```bash
make up                                    # platform-db :5434, test-db :5433, target-db (sampel) :5435
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

## Cara pakai singkat

1. **Admin**: Koneksi -> tambah koneksi -> Grup -> buat grup dan anggota -> atur akses database grup.
2. **User**: Database -> pilih database -> lihat struktur dan data -> Migration -> buat draft, tinjau, jalankan.

Halaman migration: `/databases/{id}/migrations` (tombol "Migration" di halaman database).

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
| Eksekusi | Satu transaction di database target: commit bila seluruh SQL berhasil, rollback bila ada yang gagal. |
| Statement timeout | 30 detik (`SET LOCAL statement_timeout`); menunggu lock tabel maksimal 10 detik (`lock_timeout`). Nilai ada di kode, tidak diatur lewat environment. |
| Eksekusi paralel | Advisory lock per database di platform DB. Jika sudah dipegang, eksekusi kedua langsung ditolak (HTTP 409) dan migration tetap draft. |
| Target tidak dapat dihubungi | Dicek sebelum nomor versi dipakai; migration tetap draft dan user melihat halaman 503 tanpa host/port. |
| Proses mati saat eksekusi | Saat startup, migration berstatus "running" diubah menjadi gagal. |
| Peninjauan | Halaman review membawa checksum SQL; bila SQL berubah sebelum dijalankan, eksekusi ditolak (409). |
| Validasi | Judul wajib (maks 100 karakter), SQL wajib (maks 100 KB). `BEGIN`, `COMMIT`, `ROLLBACK`, `SAVEPOINT`, `START TRANSACTION` ditolak karena transaction dikelola platform. |
| Hak akses | Lihat riwayat dan detail: View Only. Buat, edit, tinjau, jalankan: View + Edit. Dicek di backend pada setiap request. |
| Hapus koneksi | Diblokir bila punya riwayat migration (foreign key `RESTRICT`, HTTP 409). |
| Logging | Hanya id (user, database, migration) dan status. SQL, password, dan session id tidak dicatat. |

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
- `count(*)` pada tabel yang sangat besar dapat melewati timeout viewer 5 detik dan menampilkan halaman 500.

## Test

```bash
make test-unit          # tanpa database (-short)
make test-integration   # butuh `make up`; gagal (bukan skip) bila database tidak ada
make test               # keduanya
make test-integration PKG=./internal/migration   # satu paket
```

Jalankan lewat Makefile, bukan `go test` langsung, karena variabel `TEST_DATABASE_URL` dan
`TARGET_DB_URL` diisi di sana. Test migration membuat tabel `zz_mig_*` di database sampel dan
menghapusnya lagi; bila ada sisa, jalankan `make reset-target`.

## Struktur

```
cmd/server            titik masuk
internal/app          handler HTTP dan routing
internal/auth         login, session, CSRF
internal/authz        kebijakan akses (satu-satunya tempat keputusan izin)
internal/users        user dan grup
internal/dbconn       koneksi target, enkripsi password, grant
internal/explorer     introspeksi schema dan viewer data
internal/migration    migration: repository, service, executor
internal/platformdb   koneksi dan migrasi schema platform
internal/web          renderer dan template
internal/testutil     helper test
internal/db           seed database sampel
```

## Pemecahan masalah

| Gejala | Penyebab / solusi |
|---|---|
| `belum ada admin, isi ADMIN_PASSWORD` | Run pertama butuh `ADMIN_PASSWORD` (min. 8 karakter). |
| `APP_ENCRYPTION_KEY tidak valid` | Buat kunci dengan `openssl rand -base64 32`. |
| `address already in use` atau `password authentication failed` pada port 5433, 5434, atau 5435 | Port dipakai PostgreSQL lain di mesin Anda. Hentikan proses itu, atau pakai database lain untuk test: `make test TEST_DATABASE_URL=postgres://...`. |
| Test viewer gagal setelah test migration | `make reset-target` |
| Docker `permission denied` | Jalankan dengan `sudo`. |

## Tim

| Anggota | NIM | Pembagian Tugas |
|---|---|---|
| Helena Kristela Sarhawa | 13524109 | Access Control, Autentikasi, Sesi, Otorisasi, Manajemen User & Grup |
| Vara Azzara Ramli Pulukadang | 13524091 | Database Management, Encrypted Connection, Metadata Explorer, Data Viewer |
| Keisha Rizka Syofyani | 13524073 | Migration Engine, Transaksi & Locking, History, Dokumentasi, README |
