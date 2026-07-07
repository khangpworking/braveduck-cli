#!/usr/bin/env python3
"""Optional browser fetcher for braveduck-cli's "Extract & Add".

Some job sites (notably TopCV) sit behind Cloudflare and inject their
schema.org/JobPosting via JavaScript, so plain HTTP returns a 403 with no
structured data. This script fetches such a page with CloakBrowser (stealth
Chromium) and prints the fully-rendered HTML to stdout, so the braveduck binary
can parse the JobPosting exactly as it would from a plain-HTTP page.

It is entirely opt-in: braveduck-cli stays a dependency-free single Go binary,
and only shells out to this script — after plain HTTP fails — when you point it
here. The browser stack lives here, not in the binary.

Setup:
    pip install cloakbrowser        # then, once, to download the browser:
    python -m cloakbrowser download

Enable (braveduck reads this env var and runs the command via `sh -c`):
    export BRAVEDUCK_BROWSER_FETCHER='python3 /path/to/scripts/cloak-fetch.py'

braveduck passes the target URL in BRAVEDUCK_FETCH_URL and runs this at most
once per add (only on plain-HTTP failure), so the browser launches lazily.
CloakBrowser solves neither CAPTCHAs nor pure IP bans — it clears fingerprint /
Cloudflare-Turnstile checks only.
"""
import os
import sys

import cloakbrowser

# The JSON-LD block is often injected after load; wait briefly for it, but don't
# fail the fetch if it never appears — braveduck falls back to the bare URL.
WAIT_SELECTOR = 'script[type="application/ld+json"]'
BLOCK = {"image", "media", "font", "stylesheet"}  # HTML + JSON-LD is all we need


def main():
    url = os.environ.get("BRAVEDUCK_FETCH_URL", "").strip()
    if not url:
        sys.exit("BRAVEDUCK_FETCH_URL is not set")

    browser = cloakbrowser.launch(headless=True, stealth_args=True, humanize=True)
    try:
        ctx = browser.new_context(locale="vi-VN", timezone_id="Asia/Ho_Chi_Minh")
        ctx.route(
            "**/*",
            lambda route: route.abort()
            if route.request.resource_type in BLOCK
            else route.continue_(),
        )
        page = ctx.new_page()
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=45000)
            try:
                page.wait_for_selector(WAIT_SELECTOR, timeout=8000)
            except Exception:
                pass
            sys.stdout.write(page.content())
        finally:
            if not page.is_closed():
                page.close()
            ctx.close()
    finally:
        browser.close()


if __name__ == "__main__":
    main()
