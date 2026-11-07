# braveduck online with Cloudflare (free)

One braveduck on your own computer, for you and your friends, at an address
like `https://app.example.com`, with no server to rent and no ports to open:

```
browser ──► app.example.com ──► Cloudflare Access (email code) ──► Tunnel ──► braveduck on your computer
AI agent ──► app.example.com/mcp ──► (Access bypassed) ──► Tunnel ──► braveduck checks the agent key
```

- **Cloudflare Tunnel** carries requests from Cloudflare to braveduck, which
  only listens on `127.0.0.1`. No open ports, no public IP needed.
- **Cloudflare Access** logs people in with a one-time code sent to their
  email (free for up to 50 users). It signs a token on every request it lets
  through; braveduck checks the token and gives each email its own board.
- Job sites see your normal home connection, and Chromium, data and CVs stay
  on your disk. The board is online while your computer is on.

The other way to run it for several people, on a server with Caddy and
Authelia, is in [`deploy/hosted`](../hosted).

## 1. Cloudflare

In the Cloudflare dashboard, with your domain on Cloudflare:

1. **Tunnel** (Zero Trust → Networks → Tunnels): create a tunnel, install
   `cloudflared` where braveduck runs and start it with the tunnel's token.
   Add a public hostname `app.example.com` → `http://127.0.0.1:8090`.
2. **Access application** (Zero Trust → Access → Applications → Self-hosted)
   for `app.example.com`:
   - an **Allow** policy listing the emails of the people who may use it;
   - a long session (e.g. 1 month), so nobody logs in every day;
   - a second application, or a path rule, for `app.example.com/mcp` with a
     **Bypass** policy: AI agents can't receive an email code, and braveduck
     checks their agent key there instead.
3. Note the **team domain** (Zero Trust → Settings → Custom Pages:
   `TEAM.cloudflareaccess.com`) and the application's **Audience (AUD) tag**
   (the application's Overview tab). Neither is secret.

## 2. braveduck

```bash
braveduck serve --users ~/braveduck-users --addr 127.0.0.1:8090 \
  --access-team https://TEAM.cloudflareaccess.com --access-aud AUD_TAG
```

Add `--owner YOUR_EMAIL` when braveduck runs on your own computer: windows
then open on your own screen, so logging in to job sites, assisted apply and
CV files by path stay on for you (for everyone else they're off).

Each person gets `~/braveduck-users/<their email>/` the first time they log
in (`--max-users`, default 50, caps how many). The usual limits for a shared
server apply: job-site logins and assisted apply are off (they open windows
on the server), crawls run at most hourly, and Jev ratings are capped per
user per day (`--jev-daily-limit`).

To keep it running, a systemd user unit (`~/.config/systemd/user/braveduck-online.service`):

```ini
[Unit]
Description=braveduck for friends (Cloudflare Tunnel + Access)
After=network-online.target

[Service]
ExecStart=%h/go/bin/braveduck serve --users %h/braveduck-users --addr 127.0.0.1:8090 --access-team https://TEAM.cloudflareaccess.com --access-aud AUD_TAG --owner YOUR_EMAIL
Restart=on-failure

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload && systemctl --user enable --now braveduck-online
```

On WSL, user services run while a WSL session is open; `loginctl
enable-linger $USER` keeps them running without one.

## Check it

- `https://app.example.com` asks for your email, then shows your own board.
- `curl -i https://app.example.com/healthz` from anywhere: Access answers
  with its login page, not braveduck.
- `curl -i http://127.0.0.1:8090/` on the computer itself: `401 not logged
  in` — without a token Access signed, nobody gets a board, even locally.
