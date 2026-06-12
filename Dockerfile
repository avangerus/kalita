FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /kalita ./cmd/kalita

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
USER kalita
WORKDIR /opt/kalita
EXPOSE 8443
# The node generates a self-signed cert on first boot (KALITA_TLS=self-signed)
# and serves HTTPS itself. Override --pack to run a different module.
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD ["serve", "--listen", ":8443", "--data-dir", "/data", "--pack", "/opt/kalita/packs/contracts", "--ui-dir", "/opt/kalita/web"]
