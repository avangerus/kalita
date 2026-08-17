FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# OPS-1 (Cycle-F1): the binary's version is injected at build time via
# -X main.version — otherwise every image reports the "0.1.0-dev" default and
# `kalita upgrade`'s journal version guard (upgrade.go) compares against a lie.
# Build with: docker build --build-arg VERSION=v1.0.0 ...
ARG VERSION=0.1.0-dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /kalita ./cmd/kalita

FROM alpine:3.20
RUN apk add --no-cache openssl ca-certificates \
    && adduser -D -H kalita \
    && mkdir -p /data/tls && chown -R kalita /data
COPY --from=build /kalita /usr/local/bin/kalita
COPY deploy/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
COPY examples /opt/kalita/examples
COPY packs /opt/kalita/packs
COPY web /opt/kalita/web
# packages/ (kalita-ui/*.js) is a SIBLING of web/, not inside it: the buildless
# UI imports preact+htm and the atoms/molecules from /packages/... (see the
# import map in web/index.html and the /packages/ mount in cmd/kalita/main.go
# gated on --ui-dir). Without this copy the node serves index.html but every
# /packages/kalita-ui/*.js 404s → the module graph never loads → white screen.
COPY packages /opt/kalita/packages
USER kalita
WORKDIR /opt/kalita
EXPOSE 8443
# The node generates a self-signed cert on first boot (KALITA_TLS=self-signed)
# and serves HTTPS itself. Override --pack to run a different module.
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["serve", "--listen", ":8443", "--data-dir", "/data", "--pack", "/opt/kalita/packs/contracts", "--ui-dir", "/opt/kalita/web"]
