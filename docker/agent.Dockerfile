FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git make linux-headers

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /vpnbuilder-agent ./cmd/agent

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata iproute2 wireguard-tools iptables-nft nftables curl

WORKDIR /app

COPY --from=builder /vpnbuilder-agent /usr/local/bin/vpnbuilder-agent

EXPOSE 8081

HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD curl -f http://localhost:8081/healthz || exit 1

ENTRYPOINT ["vpnbuilder-agent"]
