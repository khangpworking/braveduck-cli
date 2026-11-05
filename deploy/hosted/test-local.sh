#!/bin/sh
# test-local.sh — run the whole shared-server chain on this machine, without
# Docker, and check it: real Caddy and Authelia (official release binaries,
# checksums verified), braveduck built from this checkout, the Caddyfile and
# Authelia configuration from this folder.
#
# Needs: go, curl, sha256sum/sha512sum, python3; ports 8443, 9091 and 8096
# free. Everything lives in a temporary directory, removed afterwards.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
CADDY_VERSION=${CADDY_VERSION:-2.11.9}
AUTHELIA_VERSION=${AUTHELIA_VERSION:-4.39.28}

work=$(mktemp -d)
pids=""
cleanup() {
	for p in $pids; do kill "$p" 2>/dev/null || true; done
	rm -r "$work"
}
trap cleanup EXIT INT TERM

echo "== downloading Caddy $CADDY_VERSION and Authelia $AUTHELIA_VERSION"
cd "$work"
c=caddy_${CADDY_VERSION}_linux_amd64.tar.gz
curl -fsSLO "https://github.com/caddyserver/caddy/releases/download/v$CADDY_VERSION/$c"
curl -fsSL "https://github.com/caddyserver/caddy/releases/download/v$CADDY_VERSION/caddy_${CADDY_VERSION}_checksums.txt" | grep " $c\$" | sha512sum -c -
a=authelia-v$AUTHELIA_VERSION-linux-amd64.tar.gz
curl -fsSLO "https://github.com/authelia/authelia/releases/download/v$AUTHELIA_VERSION/$a"
curl -fsSL "https://github.com/authelia/authelia/releases/download/v$AUTHELIA_VERSION/checksums.sha256" | grep " $a\$" | sha256sum -c -
tar -xzf "$c" caddy
tar -xzf "$a" authelia
(cd "$repo" && CGO_ENABLED=0 go build -o "$work/braveduck" ./cmd/braveduck)

echo "== configuring (jobs.braveduck.test / auth.braveduck.test on :8443)"
mkdir -p secrets config users
for s in proxy_secret session_secret storage_encryption_key jwt_secret; do
	od -An -N48 -tx1 /dev/urandom | tr -d ' \n' >"secrets/$s"
done
# Port 8443 instead of 443, certificates from Caddy's own local CA.
{
	printf '{\n\tlocal_certs\n\thttp_port 8081\n\thttps_port 8443\n\tadmin off\n}\n'
	sed '/^{$/,/^}$/d' "$here/Caddyfile"
} >Caddyfile
sed -e "s#/config/#$work/config/#" \
	-e 's#https://{{ env "AUTH_DOMAIN" }}#&:8443#' \
	-e 's#https://{{ env "BRAVEDUCK_DOMAIN" }}#&:8443#' \
	"$here/authelia/configuration.yml" >config/configuration.yml
printf 'users:\n' >config/users_database.yml
for u in alice bob; do
	AUTHELIA="$work/authelia" USERS_DB="$work/config/users_database.yml" BRAVEDUCK_NEW_PASSWORD="$u-password-123" \
		"$here/add-user.sh" $u $u $u@example.com >/dev/null
done

export X_AUTHELIA_CONFIG_FILTERS=template
export BRAVEDUCK_DOMAIN=jobs.braveduck.test AUTH_DOMAIN=auth.braveduck.test COOKIE_DOMAIN=braveduck.test BRAVEDUCK_POLICY=one_factor ACME_EMAIL=admin@example.com
export AUTHELIA_SESSION_SECRET_FILE="$work/secrets/session_secret"
export AUTHELIA_STORAGE_ENCRYPTION_KEY_FILE="$work/secrets/storage_encryption_key"
export AUTHELIA_IDENTITY_VALIDATION_RESET_PASSWORD_JWT_SECRET_FILE="$work/secrets/jwt_secret"
export AUTH_UPSTREAM=127.0.0.1:9091 BRAVEDUCK_UPSTREAM=127.0.0.1:8096 SECRETS_DIR="$work/secrets"

./authelia --config config/configuration.yml >authelia.log 2>&1 &
pids="$pids $!"
XDG_DATA_HOME=$work/xdg XDG_CONFIG_HOME=$work/xdg ./caddy run --config Caddyfile --adapter caddyfile >caddy.log 2>&1 &
pids="$pids $!"
HOME=$work TYPESAFE_API_KEY="" ./braveduck serve --users "$work/users" --addr 127.0.0.1:8096 \
	--proxy-secret-file secrets/proxy_secret --logout-url https://auth.braveduck.test:8443/logout >braveduck.log 2>&1 &
pids="$pids $!"
sleep 4

# c is curl reaching both test domains on this machine (Caddy's local CA).
c() { curl --resolve jobs.braveduck.test:8443:127.0.0.1 --resolve auth.braveduck.test:8443:127.0.0.1 -sk "$@"; }
J=https://jobs.braveduck.test:8443
A=https://auth.braveduck.test:8443
fails=0
check() { # check NAME WANT GOT
	if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: want $2, got $3"; fails=$((fails + 1)); fi
}
login() {
	c -c "$1.jar" -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
		-d "{\"username\":\"$1\",\"password\":\"$1-password-123\"}" "$A/api/firstfactor"
}
token() { c -b "$1.jar" "$J/" | sed -n 's/.*braveduck-token" content="\([0-9a-f]*\)".*/\1/p'; }
count() { c -b "$1.jar" -H "Authorization: Bearer $2" "$J/api/jobs" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))'; }

echo "== checks"
check "anonymous visitors are sent to the login page" 302 "$(c -o /dev/null -w '%{http_code}' "$J/")"
check "a forged Remote-User doesn't get in" 302 "$(c -o /dev/null -w '%{http_code}' -H 'Remote-User: alice' "$J/api/setup")"
check "braveduck's port refuses requests without the proxy secret" 403 "$(curl -s -o /dev/null -w '%{http_code}' -H 'Remote-User: alice' http://127.0.0.1:8096/)"
check "a wrong password is refused" 401 "$(c -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{"username":"alice","password":"wrong"}' "$A/api/firstfactor")"
check "alice logs in" 200 "$(login alice)"
check "bob logs in" 200 "$(login bob)"
ta=$(token alice)
tb=$(token bob)
check "each user gets their own board token" yes "$([ -n "$ta" ] && [ -n "$tb" ] && [ "$ta" != "$tb" ] && echo yes || echo no)"
check "alice adds a job" 201 "$(c -b alice.jar -H "Authorization: Bearer $ta" -H 'Content-Type: application/json' -d '{"content":"Go Developer at Duck Co\nHà Nội"}' -o /dev/null -w '%{http_code}' "$J/api/jobs")"
check "alice sees her job" 1 "$(count alice "$ta")"
check "bob doesn't see it" 0 "$(count bob "$tb")"
check "bob can't pose as alice with Remote-User" bob "$(c -b bob.jar -H 'Remote-User: alice' -H "Authorization: Bearer $tb" "$J/api/setup" | python3 -c 'import json,sys; print(json.load(sys.stdin)["hosted"]["user"])')"
check "alice's token doesn't open bob's board" 401 "$(c -b bob.jar -H "Authorization: Bearer $ta" -o /dev/null -w '%{http_code}' "$J/api/jobs")"
check "users' data dirs are private" 700 "$(stat -c %a users/alice)"
mcp() { # mcp AUTHORIZATION: what an AI agent gets listing its tools
	c -H "Authorization: $1" -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
		-d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' "$J/mcp"
}
code() { # code AUTHORIZATION: the HTTP status an AI agent gets
	c -o /dev/null -w '%{http_code}' -H "Authorization: $1" -H 'Content-Type: application/json' \
		-H 'Accept: application/json, text/event-stream' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' "$J/mcp"
}
key=$(c -b alice.jar -H "Authorization: Bearer $ta" -X POST "$J/api/agent-key" | python3 -c 'import json,sys; print(json.load(sys.stdin)["key"])')
check "alice makes a key for her AI agent" yes "$(case "$key" in alice:*) echo yes ;; *) echo no ;; esac)"
check "her agent reaches her MCP with the key alone" yes "$(mcp "Bearer $key" | python3 -c 'import json,sys; n=[t["name"] for t in json.load(sys.stdin)["result"]["tools"]]; print("yes" if "submit_rating" in n else "no")')"
check "the agent isn't offered upload_cv (it reads server files)" no "$(mcp "Bearer $key" | python3 -c 'import json,sys; n=[t["name"] for t in json.load(sys.stdin)["result"]["tools"]]; print("yes" if "upload_cv" in n else "no")')"
check "her password doesn't open /mcp (keys only)" 401 "$(code "Basic $(printf 'alice:alice-password-123' | base64)")"
check "bob's name on alice's key is refused" 401 "$(code "Bearer bob:${key#alice:}")"
check "a login cookie alone doesn't open /mcp" 401 "$(c -b alice.jar -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{}' "$J/mcp")"
c -b alice.jar -c alice.jar -o /dev/null "$A/logout"
check "after logging out the board is closed" 401 "$(c -b alice.jar -o /dev/null -w '%{http_code}' "$J/api/setup")"

if [ "$fails" -ne 0 ]; then
	echo "== $fails check(s) failed; logs:"
	tail -n 5 authelia.log caddy.log braveduck.log
	exit 1
fi
echo "== all checks passed"
