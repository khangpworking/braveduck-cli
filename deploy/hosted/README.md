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
| [go-rod's browser service](https://github.com/go-rod/rod) | Chromium, for reading job pages and making CV PDFs. It renders other sites' pages, so it runs in a container of its own: no users' data, no secrets, no route to the login service. |

## Before you start

- A Linux server with [Docker](https://docs.docker.com/engine/install/) and
  ports 80 and 443 open to the internet.
- A domain with two names pointing (DNS A/AAAA records) at the server, for
  example `jobs.example.com` (the board) and `auth.example.com` (the login
  page). They must be siblings under one domain: the login cookie covers both.
  **Put both under a domain nothing else uses**, e.g. `board.hunt.example.com`
  and `auth.hunt.example.com` (cookie domain `hunt.example.com`): the login
  cookie is sent to every name under that domain, so another site there could
  read it.
- Memory: each user's board takes about 3 MB while idle (measured: 50 users
  ≈ 170 MB, 200 users ≈ 580 MB for braveduck), plus up to 2 GB for the
  browser container while it renders pages (`BRAVEDUCK_MAX_BROWSERS` at
  once). A 2-4 GB server is plenty for a few dozen people.

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
- **Remove someone:** `./remove-user.sh USERNAME`. They can't log in any
  more, and their board is archived as `users/.removed-USERNAME-<time>` in the
  `braveduck_data` volume (delete it there when you no longer need it), so
  someone added later under the same name starts with an empty board.
- **Reset a password:** edit their `password:` line in
  `authelia/users_database.yml` with a new hash from
  `docker run --rm authelia/authelia:4.39 authelia crypto hash generate argon2`
  (removing and re-adding them would archive their board).
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
| `BRAVEDUCK_MAX_JOBS_PER_USER` | 5000 | Jobs each board holds; past it, adding asks to delete old ones. |
| `BRAVEDUCK_MEM_LIMIT` | 2g | Memory for the braveduck container (all users' boards). |
| `BRAVEDUCK_JEV_DAILY_LIMIT` | 50 | Jev calls (ratings and lead checks) per user per day on that key; 0 = no limit. Past it, rating waits for the next day. |

## What works differently on a shared server

- **Job-site logins and assisted apply are off.** Both open a browser window
  that the person must see and use; on a server that window would open where
  nobody can see it. The board says so and offers neither. To apply through
  your accounts, run braveduck on your own computer.
- **AI agents** connect over HTTPS to `https://BOARD_DOMAIN/mcp` with a key
  each person makes on their board (**Ask your AI → Make a key for your
  agent**), never their password. The board shows the command once, e.g.:

  ```bash
  claude mcp add --transport http braveduck https://jobs.example.com/mcp \
    --header "Authorization: Bearer alice:3f9c…"
  ```

  Only the key's hash is stored; making a new key or **Remove the key**
  stops the old one. Tools that read files on the machine braveduck runs on
  (`upload_cv`) or open windows there (`connect_platform`, `apply_job`) are
  left out; upload the CV on the board instead.
- **Session keys** are a file in each user's folder, never the server's
  keychain or a shared passphrase, so one user's saved logins can't be opened
  with another's key.

## Where the browser runs

Job pages are other people's code. Chromium runs them in the `browser`
container (go-rod's `rod-manager`), not next to the users' data: braveduck
asks it for a browser over the private `browser` network and the browser is
killed when braveduck lets go. That container has no volume, a read-only
filesystem apart from `/tmp`, no Linux capabilities, and memory and process
limits; it can reach the internet but not Caddy or Authelia. On a cloud
server, also block the provider's metadata address (169.254.169.254) for
containers if your provider doesn't.

## How braveduck trusts Caddy

braveduck takes the user's name from the `Remote-User` header, which Authelia
sets after checking the login cookie. Caddy first deletes any `Remote-User`
the browser sent, then adds a secret (`secrets/proxy_secret`) that braveduck
checks on every request, and keeps the login cookie to itself. `/mcp` skips
Authelia: there braveduck checks the agent key, which names its user. A request that reaches braveduck any other way,
without the secret, is refused. braveduck's port is never published outside
the compose network.

## Testing the setup

`./test-local.sh` runs the whole chain on one machine without Docker: the
official Caddy and Authelia release binaries (checksums verified), braveduck
built from this checkout, and this folder's configuration. It checks logins,
that users can't see or pose as each other, agent keys, and that braveduck
refuses requests that bypass Caddy.

`./test-compose.sh` starts this folder's Docker Compose stack for real
(images built, `setup.sh` and `add-user.sh` run, Caddy on its local CA instead
of Let's Encrypt) and checks it from outside, including that braveduck can
run Chromium in its container. It refuses to run in a folder that is already
set up. CI runs both on every change.
