# Running braveduck for several people

braveduck normally runs on your own computer, for you alone. This folder
turns it into a shared server: each person logs in and gets their own board,
with their own jobs, CV, profile, search terms, crawls and backups. Nobody
sees anyone else's data.

braveduck doesn't handle logins itself; two well-known tools do:

| Piece | What it does |
|---|---|
| [Caddy](https://caddyserver.com) | The web server people reach. Gets HTTPS certificates from Let's Encrypt by itself, and asks Authelia about every request. |
| [Authelia](https://www.authelia.com) | The login page. Passwords (stored only as argon2id hashes), optional two-factor, lock-out after repeated wrong passwords. |
| braveduck | `braveduck serve --users DIR`: one complete board per user, in `DIR/<user>/`. Only Caddy can reach it. |

## Before you start

- A Linux server with [Docker](https://docs.docker.com/engine/install/) and
  ports 80 and 443 open to the internet.
- A domain with two names pointing (DNS A/AAAA records) at the server, for
  example `jobs.example.com` (the board) and `auth.example.com` (the login
  page). They must be siblings under one domain: the login cookie covers both.
- About 1 GB of memory for every two crawls running at once (see
  `BRAVEDUCK_MAX_BROWSERS`).

## Set up

```bash
cd deploy/hosted
./setup.sh jobs.example.com auth.example.com you@example.com
./add-user.sh alice 'Alice Nguyen' alice@example.com   # asks for her password
docker compose up -d --build
```

`setup.sh` writes `.env` (the domains and limits) and random secrets in
`secrets/`. Keep both out of version control (`.gitignore` already does).

Open `https://jobs.example.com`: you get Authelia's login page, then your
board. The board shows who is logged in, with a **Log out** link.

## Day to day

- **Add someone:** `./add-user.sh USERNAME 'Full Name' EMAIL`. No restart:
  Authelia notices the change. Their board is created the first time they log
  in.
- **Remove someone:** delete their entry from `authelia/users_database.yml`.
  Their board data stays in the `braveduck_data` volume under `users/USERNAME`
  until you delete it.
- **Reset a password:** delete the person's entry and add them again.
- **Two-factor:** set `BRAVEDUCK_POLICY=two_factor` in `.env` and restart
  Authelia (`docker compose up -d`). People then register an authenticator app
  or passkey; the one-time codes for that land in
  `authelia/notification.txt` unless you
  [set up email](https://www.authelia.com/configuration/notifications/smtp/).
- **Update:** `git pull && docker compose up -d --build`.
- **Logs:** `docker compose logs -f braveduck` (or `caddy`, `authelia`).

## Backups

Every user's board backs itself up daily into their own folder (the last 7
days). To copy everything off the server, stop braveduck briefly and archive
the volume:

```bash
docker compose stop braveduck
docker run --rm -v braveduck_braveduck_data:/data -v "$PWD":/out ubuntu tar -czf /out/braveduck-data.tar.gz -C /data .
docker compose start braveduck
```

Also keep `authelia/users_database.yml`, `.env` and `secrets/`.

## Settings (`.env`)

| Variable | Default | What it does |
|---|---|---|
| `BRAVEDUCK_DOMAIN` | — | The board's address. |
| `AUTH_DOMAIN` | — | The login page's address. |
| `COOKIE_DOMAIN` | — | Their shared parent domain. |
| `ACME_EMAIL` | — | Let's Encrypt's contact for certificate problems. |
| `BRAVEDUCK_POLICY` | `one_factor` | `two_factor` to require a second factor. |
| `BRAVEDUCK_MAX_USERS` | 50 | New people past this are told the server is full. |
| `BRAVEDUCK_MAX_BROWSERS` | 2 | Crawls, page fetches and PDFs running at once, for everyone together. |
| `TYPESAFE_API_KEY` | empty | Jev rates every user's new jobs with this key, so it pays for all of them. Empty: people rate with their own AI agents. |

## What works differently on a shared server

- **Job-site logins and assisted apply are off.** Both open a browser window
  that the person must see and use; on a server that window would open where
  nobody can see it. The board says so and offers neither. To apply through
  your accounts, run braveduck on your own computer.
- **The MCP server** (`braveduck mcp`, for AI agents) runs only on your own
  computer: it talks over stdin, not the network.
- **Session keys** are a file in each user's folder, never the server's
  keychain or a shared passphrase, so one user's saved logins can't be opened
  with another's key.

## How braveduck trusts Caddy

braveduck takes the user's name from the `Remote-User` header, which Authelia
sets after checking the login cookie. Caddy first deletes any `Remote-User`
the browser sent, then adds a secret (`secrets/proxy_secret`) that braveduck
checks on every request. A request that reaches braveduck any other way,
without the secret, is refused. braveduck's port is never published outside
the compose network.

## Testing the setup

`./test-local.sh` runs the whole chain on one machine without Docker: the
official Caddy and Authelia release binaries (checksums verified), braveduck
built from this checkout, and this folder's configuration. It checks logins,
that users can't see or pose as each other, and that braveduck refuses
requests that bypass Caddy. CI runs it on every change, and builds the image.
