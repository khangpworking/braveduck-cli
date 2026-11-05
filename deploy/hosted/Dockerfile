# braveduck shared server image. Build from the repository root:
#   docker build -f deploy/hosted/Dockerfile .
# (compose.yaml does this for you.)

FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/braveduck ./cmd/braveduck

# go-rod's own image: Ubuntu with the libraries, fonts (CJK and emoji
# included) and dumb-init a headless Chromium needs. braveduck downloads its
# CloakBrowser build into the data volume on first use.
FROM ghcr.io/go-rod/rod:v0.116.2
RUN useradd --create-home --uid 10001 braveduck && mkdir -p /data && chown braveduck:braveduck /data
COPY --from=build /out/braveduck /usr/local/bin/braveduck
USER braveduck
ENV HOME=/data CLOAKBROWSER_CACHE_DIR=/data/cloakbrowser
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["dumb-init", "--", "braveduck"]
CMD ["serve", "--users", "/data/users", "--addr", "0.0.0.0:8080", "--proxy-secret-file", "/run/secrets/proxy_secret"]
