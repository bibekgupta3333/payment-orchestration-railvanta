# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
RUN apk add --no-cache build-base
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 go build -mod=readonly -trimpath \
    -ldflags="-s -w -linkmode=external -extldflags=-static" \
    -o /out/railvanta-api ./cmd/api

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 railvanta \
    && adduser -D -u 10001 -G railvanta railvanta \
    && mkdir -p /var/lib/railvanta \
    && chown railvanta:railvanta /var/lib/railvanta
COPY --from=build /out/railvanta-api /usr/local/bin/railvanta-api
USER 10001:10001
WORKDIR /var/lib/railvanta
ENV APP_ENV=sandbox \
    HTTP_ADDR=:8080 \
    HEALTH_ADDR=127.0.0.1:8081 \
    SQLITE_PATH=/var/lib/railvanta/railvanta.db \
    DEMO_MODE=false
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget -q -O /dev/null http://127.0.0.1:8081/health/ready || exit 1
ENTRYPOINT ["/usr/local/bin/railvanta-api"]
