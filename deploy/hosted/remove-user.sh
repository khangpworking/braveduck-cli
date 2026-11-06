#!/bin/sh
# remove-user.sh USERNAME — stop someone logging in, and archive their board.
# Their data moves to users/.removed-USERNAME-<time> in the braveduck volume
# (delete it there when you no longer need it), so someone added later under
# the same name starts with an empty board. Restarts braveduck to stop the
# old board's crawls.
set -eu
cd "$(dirname "$0")"
COMPOSE=${COMPOSE:-docker compose}

if [ $# -ne 1 ]; then
	echo "usage: $0 USERNAME" >&2
	exit 2
fi
user=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
if ! printf '%s' "$user" | grep -Eq '^[a-z0-9][a-z0-9._@-]{0,63}$'; then
	echo "not a user name: $1" >&2
	exit 2
fi
db=${USERS_DB:-authelia/users_database.yml}
if ! grep -Fqx "  $user:" "$db"; then
	echo "$user isn't in $db" >&2
	exit 1
fi

# Archive the board first: if that fails, nothing has changed yet.
stamp=$(date +%Y%m%d-%H%M%S)
$COMPOSE exec -T braveduck sh -c "if [ -d '/data/users/$user' ]; then mv '/data/users/$user' '/data/users/.removed-$user-$stamp'; fi"

# Drop the user's block: from "  USER:" up to the next user (two-space
# indent) or the end of the file. Authelia notices the change.
tmp=$(mktemp "$db.XXXXXX")
awk -v u="  $user:" '
	$0 == u { skip = 1; next }
	skip && /^  [^ ]/ { skip = 0 }
	!skip { print }
' "$db" >"$tmp"
cat "$tmp" >"$db" # keep the file (and its owner) in place
rm -f "$tmp"

$COMPOSE restart braveduck >/dev/null
echo "Removed $user: they can't log in, and their board is archived as users/.removed-$user-$stamp."
