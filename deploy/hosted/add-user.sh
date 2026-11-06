#!/bin/sh
# add-user.sh USERNAME 'Full Name' EMAIL — let someone log in. Asks for their
# password and stores only its argon2id hash (made by Authelia itself).
# Authelia notices the change; no restart needed. Remove someone by deleting
# their entry from authelia/users_database.yml (their board's data stays in
# the braveduck volume under users/USERNAME).
set -eu
cd "$(dirname "$0")"
AUTHELIA=${AUTHELIA:-docker run --rm authelia/authelia:4.39 authelia}

if [ $# -ne 3 ]; then
	echo "usage: $0 USERNAME 'Full Name' EMAIL" >&2
	exit 2
fi
user=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]') name=$2 email=$3
# The same rule braveduck applies: the name becomes a folder.
if ! printf '%s' "$user" | grep -Eq '^[a-z0-9][a-z0-9._@-]{0,63}$'; then
	echo "user names are letters, digits, . _ @ - (up to 64), starting with a letter or digit" >&2
	exit 2
fi
case "$name$email" in
*"'"* | *'"'* | *[[:cntrl:]]*)
	echo "the name and email can't contain quotes or line breaks" >&2
	exit 2
	;;
esac
db=${USERS_DB:-authelia/users_database.yml}
[ -e "$db" ] || printf 'users:\n' >"$db"
if grep -Fqx "  $user:" "$db"; then
	echo "$user is already in $db" >&2
	exit 1
fi

if [ -n "${BRAVEDUCK_NEW_PASSWORD:-}" ]; then
	pw=$BRAVEDUCK_NEW_PASSWORD
else
	stty -echo 2>/dev/null || true
	printf 'Password for %s (at least 12 characters): ' "$user"
	read -r pw
	stty echo 2>/dev/null || true
	echo
fi
if [ ${#pw} -lt 12 ]; then
	echo "too short: use at least 12 characters" >&2
	exit 2
fi

hash=$($AUTHELIA crypto hash generate argon2 --password "$pw" | sed -n 's/^Digest: //p')
# shellcheck disable=SC2016 # a literal "$argon2id$", the hash's prefix
case "$hash" in
'$argon2id$'*) ;;
*)
	echo "couldn't hash the password with Authelia" >&2
	exit 1
	;;
esac
cat >>"$db" <<YAML
  $user:
    disabled: false
    displayname: '$name'
    email: '$email'
    password: '$hash'
YAML
auth=$(sed -n 's/^AUTH_DOMAIN=//p' .env 2>/dev/null || true)
echo "Added $user. They log in at https://${auth:-<AUTH_DOMAIN>} and land on their own board."
