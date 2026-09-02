FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git make linux-headers

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /vpnbuilder-agent ./cmd/agent

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata iproute2 wireguard-tools iptables-nft

WORKDIR /app

COPY --from=builder /vpnbuilder-agent /usr/local/bin/vpnbuilder-agent

ENTRYPOINT ["vpnbuilder-agent"]