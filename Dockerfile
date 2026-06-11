FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /kalita ./cmd/kalita

FROM alpine:3.20
RUN adduser -D -H kalita
COPY --from=build /kalita /usr/local/bin/kalita
COPY examples /opt/kalita/examples
COPY packs /opt/kalita/packs
USER kalita
EXPOSE 8080
ENTRYPOINT ["kalita"]
CMD ["serve", "--listen", ":8080"]
