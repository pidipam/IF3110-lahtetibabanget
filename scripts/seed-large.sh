#!/usr/bin/env bash
set -euo pipefail

rows="${LARGE_ROWS:-2000000}"
url="${TARGET_DB_URL:-postgres://app:app@127.0.0.1:5435/sample?sslmode=disable}"

if [[ ! "$rows" =~ ^[1-9][0-9]*$ ]]; then
    echo "LARGE_ROWS harus bilangan bulat positif." >&2
    exit 1
fi

now_ns() {
    date +%s%N
}

elapsed_ms() {
    local start="$1"
    local end="$2"
    echo $(((end - start) / 1000000))
}

start="$(now_ns)"
psql "$url" -X -q -v ON_ERROR_STOP=1 <<SQL
DROP TABLE IF EXISTS zz_big;
CREATE TABLE zz_big (
    id bigint PRIMARY KEY,
    x integer NOT NULL DEFAULT 0,
    pad text NOT NULL
);
INSERT INTO zz_big (id, pad)
SELECT g, md5(g::text) FROM generate_series(1, $rows) AS g;
ANALYZE zz_big;
SQL
seed_ms="$(elapsed_ms "$start" "$(now_ns)")"

start="$(now_ns)"
psql "$url" -X -q -v ON_ERROR_STOP=1 -c "UPDATE zz_big SET x = 1, pad = pad || 'a'"
update_ms="$(elapsed_ms "$start" "$(now_ns)")"

psql "$url" -X -q -v ON_ERROR_STOP=1 -c "UPDATE zz_big SET x = 0, pad = md5(id::text)"
count="$(psql "$url" -X -tA -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM zz_big")"

printf 'table=zz_big id_column=id rows=%s seed_ms=%s full_update_ms=%s\n' "$count" "$seed_ms" "$update_ms"
