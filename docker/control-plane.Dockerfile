FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /vpnbuilder-cp ./cmd/control-plane

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /vpnbuilder-cp /usr/local/bin/vpnbuilder-cp

EXPOSE 8080 9090

ENTRYPOINT ["vpnbuilder-cp"]