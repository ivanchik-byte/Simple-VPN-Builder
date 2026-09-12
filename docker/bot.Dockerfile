FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /vpnbuilder-bot ./cmd/bot

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

RUN adduser -D -u 10001 -s /bin/sh vpnbuilder && \
    chown -R vpnbuilder:vpnbuilder /app

COPY --from=builder /vpnbuilder-bot /usr/local/bin/vpnbuilder-bot

USER vpnbuilder

ENTRYPOINT ["vpnbuilder-bot"]
