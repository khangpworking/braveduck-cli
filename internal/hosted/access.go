package hosted

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Cloudflare Access as the login, instead of a proxy with a shared secret:
// a Cloudflare Tunnel brings requests to braveduck's port, and Access puts a
// token it signed on every request it let through. braveduck checks that
// token (the team's public keys, the issuer, this app's audience tag, the
// expiry) and takes the user from its email. A request without a valid
// token never reaches a board, whoever sends it — so nothing else on the
// machine can pose as a user either.
//
// /mcp is left out of Access (a Bypass policy), as AI agents can't do an
// email login; there braveduck's own agent keys decide, as before.

// AccessHeader carries the token Access signed.
const AccessHeader = "Cf-Access-Jwt-Assertion"

// accessLogout is Access's own log-out page, on every protected domain.
const accessLogout = "/cdn-cgi/access/logout"

type accessVerifier struct {
	v *oidc.IDTokenVerifier
}

// newAccess checks tokens from the Access team at team (its address, e.g.
// https://myteam.cloudflareaccess.com) for the application with audience
// tag aud. Keys are fetched from the team on first use and cached.
func newAccess(ctx context.Context, team, aud string) (*accessVerifier, error) {
	team = strings.TrimRight(strings.TrimSpace(team), "/")
	if !strings.HasPrefix(team, "https://") {
		return nil, fmt.Errorf("hosted: the Access team must be its https:// address, not %q", team)
	}
	if strings.TrimSpace(aud) == "" {
		return nil, errors.New("hosted: Cloudflare Access needs the application's audience (AUD) tag")
	}
	keys := oidc.NewRemoteKeySet(ctx, team+"/cdn-cgi/access/certs")
	return &accessVerifier{v: oidc.NewVerifier(team, keys, &oidc.Config{ClientID: strings.TrimSpace(aud)})}, nil
}

// user returns the email of the user the request's Access token names, or
// an error when there is no token or it doesn't check out.
func (a *accessVerifier) user(r *http.Request) (string, error) {
	raw := r.Header.Get(AccessHeader)
	if raw == "" {
		return "", errors.New("no Access token")
	}
	tok, err := a.v.Verify(r.Context(), raw)
	if err != nil {
		return "", err
	}
	var claims struct {
		Email string `json:"email"`
	}
	if err := tok.Claims(&claims); err != nil || claims.Email == "" {
		return "", errors.New("the Access token names no email")
	}
	return strings.ToLower(strings.TrimSpace(claims.Email)), nil
}
