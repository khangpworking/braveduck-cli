#!/bin/sh
# test-compose.sh — start the real Docker Compose stack from this folder
# (Caddy, Authelia, the braveduck image) the way setup.sh and add-user.sh
# prepare it, and check it from outside. Caddy uses its own local CA instead
# of Let's Encrypt; everything else is as deployed.
#
# Needs Docker with compose, curl, python3, and ports 80/443 free. Runs only
# in a folder that hasn't been set up (no .env): it would replace a real
# server's settings. Removes what it created afterwards.
set -eu
cd "$(dirname "$0")"
if [ -e .env ] || [ -e secrets ] || [ -e authelia/users_database.yml ]; then
	echo "this folder is set up already (.env, secrets/ or users_database.yml exist): run the test on a clean checkout" >&2
	exit 1
fi

compose() { docker compose -f compose.yaml -f compose.test.yaml "$@"; }
cleanup() {
	compose down -v --remove-orphans >/dev/null 2>&1 || true
	rm -f .env Caddyfile.test compose.test.yaml authelia/users_database.yml \
		authelia/db.sqlite3 authelia/notification.txt secrets/* 2>/dev/null || true
	rmdir secrets 2>/dev/null || true
	rm -f alice.jar bob.jar
}
trap cleanup EXIT INT TERM

echo "== setting up"
./setup.sh jobs.braveduck.test auth.braveduck.test admin@example.com >/dev/null
for u in alice bob; do
	BRAVEDUCK_NEW_PASSWORD="$u-password-123" ./add-user.sh "$u" "$u" "$u@example.com" >/dev/null
done
{
	printf '{\n\tlocal_certs\n}\n'
	sed '/^{$/,/^}$/d' Caddyfile
} >Caddyfile.test
cat >compose.test.yaml <<'YAML'
services:
  caddy:
    volumes: !override
      - ./Caddyfile.test:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
YAML

echo "== starting"
compose up -d --build --wait --wait-timeout 180
sleep 3

c() { curl --resolve jobs.braveduck.test:443:127.0.0.1 --resolve auth.braveduck.test:443:127.0.0.1 -sk "$@"; }
J=https://jobs.braveduck.test
A=https://auth.braveduck.test
fails=0
check() { # check NAME WANT GOT
	if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: want $2, got $3"; fails=$((fails + 1)); fi
}
login() {
	c -c "$1.jar" -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
		-d "{\"username\":\"$1\",\"password\":\"$1-password-123\"}" "$A/api/firstfactor"
}
token() { c -b "$1.jar" "$J/" | sed -n 's/.*braveduck-token" content="\([0-9a-f]*\)".*/\1/p'; }

echo "== checks"
check "anonymous visitors are sent to the login page" 302 "$(c -o /dev/null -w '%{http_code}' "$J/")"
check "the board is served with HSTS" yes "$(c -D - -o /dev/null "$J/" | grep -qi '^strict-transport-security: max-age=' && echo yes || echo no)"
check "braveduck's port isn't published" 000 "$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 http://127.0.0.1:8080/ || true)"
check "alice logs in" 200 "$(login alice)"
check "bob logs in" 200 "$(login bob)"
ta=$(token alice)
tb=$(token bob)
check "each user gets their own board token" yes "$([ -n "$ta" ] && [ -n "$tb" ] && [ "$ta" != "$tb" ] && echo yes || echo no)"
job=$(c -b alice.jar -H "Authorization: Bearer $ta" -H 'Content-Type: application/json' \
	-d '{"content":"Go Developer at Duck Co\nHà Nội"}' "$J/api/jobs" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
check "alice adds a job" yes "$([ -n "$job" ] && echo yes || echo no)"
check "bob doesn't see it" 0 "$(c -b bob.jar -H "Authorization: Bearer $tb" "$J/api/jobs" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')"
check "bob can't pose as alice" bob "$(c -b bob.jar -H 'Remote-User: alice' -H "Authorization: Bearer $tb" "$J/api/setup" | python3 -c 'import json,sys; print(json.load(sys.stdin)["hosted"]["user"])')"
c -b alice.jar -H "Authorization: Bearer $ta" -H 'Content-Type: application/json' -X PUT \
	-d '{"markdown":"# Alice\n\nGo developer with five years of building backend services."}' -o /dev/null "$J/api/jobs/$job/cv"
pdf=$(c -b alice.jar -H "Authorization: Bearer $ta" "$J/api/jobs/$job/cv.pdf" | head -c 300 | tr -d '\0')
check "the browser service renders a CV as PDF" "%PDF" "$(printf '%s' "$pdf" | head -c 4)"
case "$pdf" in %PDF*) ;; *) echo "     the server said: $pdf" ;; esac
check "the browser container can't reach Authelia" no "$(compose exec -T browser sh -c 'getent hosts authelia >/dev/null' && echo yes || echo no)"
check "the browser container holds no users' data" no "$(compose exec -T browser sh -c 'test -e /data/users' && echo yes || echo no)"
check "Authelia leaves authelia/ owned by the admin" "$(id -u)" "$(stat -c %u authelia/users_database.yml)"
check "alice's board lives in her own private folder" 700 "$(compose exec -T braveduck sh -c 'test -s /data/users/alice/braveduck.db && stat -c %a /data/users/alice')"

if [ "$fails" -ne 0 ]; then
	echo "== $fails check(s) failed; logs:"
	compose logs --tail 20
	exit 1
fi
echo "== all checks passed"
