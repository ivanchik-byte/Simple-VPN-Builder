FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /vpnbuilder-cp ./cmd/control-plane

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

COPY --from=builder /vpnbuilder-cp /usr/local/bin/vpnbuilder-cp

EXPOSE 8110 9090

HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD curl -f http://localhost:8110/healthz || exit 1

ENTRYPOINT ["vpnbuilder-cp"]