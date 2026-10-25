#!/usr/bin/env bash
# Smoke test akses dan keamanan. Server harus sedang berjalan.
#   ADMIN_PASSWORD=... [BASE=http://localhost:8080] [LOG=/tmp/app.log] bash scripts/smoke.sh
# Membuat satu user biasa (smoke...) di database; jalankan hanya pada database dev.
set -u
BASE=${BASE:-http://localhost:8080}
ADMIN_USER=${ADMIN_USER:-admin}
ADMIN_PASSWORD=${ADMIN_PASSWORD:?isi ADMIN_PASSWORD}
LOG=${LOG:-}
pass=0; fail=0
ok()    { pass=$((pass+1)); echo "  ok    $1"; }
bad()   { fail=$((fail+1)); echo "  GAGAL $1"; }
eq()    { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (harus $2, dapat $3)"; fi; }
has()   { if printf '%s' "$2" | grep -qi -- "$3"; then ok "$1"; else bad "$1 (tidak memuat: $3)"; fi; }
hasnt() { if printf '%s' "$2" | grep -qi -- "$3"; then bad "$1 (memuat: $3)"; else ok "$1"; fi; }
code()  { curl -s -o /dev/null -w '%{http_code}' "$@"; }
csrf()  { curl -s -b "$1" -c "$1" "$2" | grep -o 'name="csrf_token" value="[^"]*"' | head -1 | sed 's/.*value="//;s/"$//'; }
login() { local t; t=$(csrf "$1" "$BASE/login")
  curl -s -o /dev/null -w '%{http_code}' -b "$1" -c "$1" --data-urlencode "csrf_token=$t" \
    --data-urlencode "username=$2" --data-urlencode "password=$3" "$BASE/login"; }

echo "[1] Tanpa login"
eq "/ diarahkan ke login" 303 "$(code "$BASE/")"
eq "/admin/users diarahkan ke login" 303 "$(code "$BASE/admin/users")"
eq "/databases/1/migrations diarahkan ke login" 303 "$(code "$BASE/databases/1/migrations")"

echo "[2] Header keamanan"
h=$(curl -sI "$BASE/login")
has "X-Frame-Options DENY" "$h" "x-frame-options: deny"
has "nosniff" "$h" "x-content-type-options: nosniff"
has "no-store" "$h" "cache-control: no-store"

echo "[3] Login gagal bersifat generik"
j=$(mktemp); t=$(csrf "$j" "$BASE/login")
b1=$(curl -s -b "$j" -c "$j" --data-urlencode "csrf_token=$t" --data-urlencode "username=$ADMIN_USER" --data-urlencode "password=salah-total-123" "$BASE/login")
b2=$(curl -s -b "$j" -c "$j" --data-urlencode "csrf_token=$t" --data-urlencode "username=tidak-ada-xyz" --data-urlencode "password=salah-total-123" "$BASE/login")
has "user ada, password salah" "$b1" "Username atau password salah."
has "user tidak ada" "$b2" "Username atau password salah."

echo "[4] Login admin dan cookie session"
a=$(mktemp); t=$(csrf "$a" "$BASE/login")
hdr=$(curl -s -D - -o /dev/null -b "$a" -c "$a" --data-urlencode "csrf_token=$t" \
  --data-urlencode "username=$ADMIN_USER" --data-urlencode "password=$ADMIN_PASSWORD" "$BASE/login")
has "login admin berhasil (303)" "$hdr" "^HTTP/[0-9.]* 303"
cookie=$(printf '%s' "$hdr" | grep -i '^set-cookie: dbmp_session')
has "cookie HttpOnly" "$cookie" "HttpOnly"
has "cookie SameSite=Lax" "$cookie" "SameSite=Lax"
hasnt "session id tidak ada di URL redirect" "$(printf '%s' "$hdr" | grep -i '^location')" "dbmp_session"

echo "[5] CSRF"
eq "POST tanpa token ditolak" 403 "$(code -b "$a" -d "name=x" "$BASE/admin/groups")"
eq "POST token salah ditolak" 403 "$(code -b "$a" -d "csrf_token=palsu&name=x" "$BASE/admin/groups")"

echo "[6] User biasa tidak boleh ke area admin atau database tanpa grant"
u="smoke$RANDOM"; pw="smokepass123"
t=$(csrf "$a" "$BASE/admin/users/new")
eq "admin membuat user" 303 "$(code -b "$a" -c "$a" --data-urlencode "csrf_token=$t" --data-urlencode "username=$u" --data-urlencode "password=$pw" "$BASE/admin/users")"
n=$(mktemp)
eq "user biasa bisa login" 303 "$(login "$n" "$u" "$pw")"
eq "/databases boleh" 200 "$(code -b "$n" "$BASE/databases")"
eq "/admin/users ditolak" 403 "$(code -b "$n" "$BASE/admin/users")"
eq "/admin/connections ditolak" 403 "$(code -b "$n" "$BASE/admin/connections")"
eq "/databases/1 tanpa akses ditolak" 403 "$(code -b "$n" "$BASE/databases/1")"
eq "/databases/1/migrations tanpa akses ditolak" 403 "$(code -b "$n" "$BASE/databases/1/migrations")"
eq "id acak tidak bisa ditebak (403, bukan 404)" 403 "$(code -b "$n" "$BASE/databases/987654")"
tn=$(csrf "$n" "$BASE/databases")
eq "POST admin dengan CSRF valid tetap ditolak" 403 "$(code -b "$n" --data-urlencode "csrf_token=$tn" --data-urlencode "name=ilegal" "$BASE/admin/groups")"
eq "POST migration tanpa akses ditolak" 403 "$(code -b "$n" --data-urlencode "csrf_token=$tn" --data-urlencode "title=x" --data-urlencode "sql=select 1" "$BASE/databases/1/migrations")"

echo "[7] Logout mematikan session di backend"
old=$(grep dbmp_session "$n" | awk '{print $NF}')
tl=$(csrf "$n" "$BASE/databases")
eq "logout" 303 "$(code -b "$n" -c "$n" --data-urlencode "csrf_token=$tl" "$BASE/logout")"
eq "cookie lama tidak berlaku lagi" 401 "$(code -H "Cookie: dbmp_session=$old" "$BASE/databases")"

echo "[8] Rahasia tidak boleh masuk log"
if [ -n "$LOG" ] && [ -f "$LOG" ]; then
  eq "password admin tidak ada di log" 0 "$(grep -c -F -- "$ADMIN_PASSWORD" "$LOG")"
  eq "password user uji tidak ada di log" 0 "$(grep -c -F -- "$pw" "$LOG")"
  eq "session id tidak ada di log" 0 "$(grep -c -F -- "$old" "$LOG")"
else
  echo "  lewati (isi LOG=/path/file untuk memeriksa)"
fi

echo; echo "Lulus: $pass   Gagal: $fail"
rm -f "$j" "$a" "$n"
[ "$fail" -eq 0 ]