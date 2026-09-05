FROM golang:1.27-alpine AS builder

RUN apk add --no-cache git make linux-headers

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /vpnbuilder-agent ./cmd/agent

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata iproute2 wireguard-tools iptables-nft nftables curl bash unzip

# Install Xray-core binary and geoip/geosite assets
ARG TARGETARCH
ARG XRAY_VERSION=v1.8.24
RUN case "${TARGETARCH}" in \
        "arm64") XRAY_ARCH="arm64-v8a" ;; \
        *) XRAY_ARCH="64" ;; \
    esac && \
    curl -sSL "https://github.com/XTLS/Xray-core/releases/download/${XRAY_VERSION}/Xray-linux-${XRAY_ARCH}.zip" -o /tmp/xray.zip && \
    unzip -q /tmp/xray.zip -d /tmp/xray && \
    mv /tmp/xray/xray /usr/local/bin/xray && \
    mkdir -p /usr/local/share/xray && \
    mv /tmp/xray/geoip.dat /usr/local/share/xray/ 2>/dev/null || true && \
    mv /tmp/xray/geosite.dat /usr/local/share/xray/ 2>/dev/null || true && \
    chmod +x /usr/local/bin/xray && \
    rm -rf /tmp/xray*

ENV XRAY_LOCATION_ASSET=/usr/local/share/xray

WORKDIR /app

COPY --from=builder /vpnbuilder-agent /usr/local/bin/vpnbuilder-agent

EXPOSE 8081 443/tcp 51820/udp

HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD curl -f http://localhost:8081/healthz || exit 1

ENTRYPOINT ["vpnbuilder-agent"]
