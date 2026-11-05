#!/bin/sh
# setup.sh BOARD_DOMAIN AUTH_DOMAIN EMAIL — first-time setup of the shared server.
#   BOARD_DOMAIN  where the board lives, e.g. jobs.example.com
#   AUTH_DOMAIN   the login page, a sibling of it, e.g. auth.example.com
#   EMAIL         for Let's Encrypt's certificate notices
# Writes .env and random secrets; never overwrites existing ones.
set -eu
cd "$(dirname "$0")"

if [ $# -ne 3 ]; then
	echo "usage: $0 BOARD_DOMAIN AUTH_DOMAIN EMAIL   (e.g. jobs.example.com auth.example.com you@example.com)" >&2
	exit 2
fi
board=$1 auth=$2 email=$3
cookie=${auth#*.} # the login cookie covers both: they must share a parent domain
case "$board" in
*."$cookie") ;;
*)
	echo "$board and $auth must be siblings under one domain (e.g. jobs.example.com and auth.example.com)" >&2
	exit 2
	;;
esac

if [ -e .env ]; then
	echo ".env already exists — edit it instead" >&2
	exit 1
fi
cat >.env <<ENV
BRAVEDUCK_DOMAIN=$board
AUTH_DOMAIN=$auth
COOKIE_DOMAIN=$cookie
ACME_EMAIL=$email
# one_factor = password; two_factor also asks for an authenticator app or passkey.
BRAVEDUCK_POLICY=one_factor
BRAVEDUCK_MAX_USERS=50
BRAVEDUCK_MAX_BROWSERS=2
# TYPESAFE_API_KEY=   # optional, see compose.yaml
ENV

# The secrets directory is private; the files are readable inside the
# containers (compose mounts them as they are on disk).
mkdir -p secrets
chmod 700 secrets
for name in proxy_secret session_secret storage_encryption_key jwt_secret; do
	if [ ! -s "secrets/$name" ]; then
		od -An -N48 -tx1 /dev/urandom | tr -d ' \n' >"secrets/$name"
	fi
	chmod 644 "secrets/$name"
done

[ -e authelia/users_database.yml ] || printf 'users:\n' >authelia/users_database.yml

echo "Done. Next:"
echo "  1. Point $board and $auth (DNS A/AAAA records) at this server."
echo "  2. Add the first person:   ./add-user.sh USERNAME 'Full Name' EMAIL"
echo "  3. Start it:               docker compose up -d --build"
