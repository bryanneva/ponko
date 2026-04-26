FROM node:22-slim AS web-builder

WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm install
COPY web/ .
RUN npm run build

FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=web-builder /app/web/dist web/dist
RUN CGO_ENABLED=0 go build -o server ./cmd/slack

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --shell /bin/false appuser
USER appuser

COPY --from=builder /app/server /usr/local/bin/server
COPY --from=builder /app/db/migrations /app/db/migrations

EXPOSE 8080

ENTRYPOINT ["server"]
