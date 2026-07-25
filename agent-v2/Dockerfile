FROM golang:1.26-alpine AS agent-builder

ENV GOTOOLCHAIN=auto

WORKDIR /build

ARG VERSION=dev
ARG GIT_COMMIT=unknown

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X github.com/bingxin666/dn42-bot/agent-v2/internal/service.BuildVersion=${VERSION} -X github.com/bingxin666/dn42-bot/agent-v2/internal/service.BuildCommit=${GIT_COMMIT}" \
    -o /agent ./cmd/agent

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    bird2 \
    wireguard-tools \
    iproute2 \
    iputils-ping \
    vnstat \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=agent-builder /agent /app/agent

COPY config.example.yaml /app/config.yaml

WORKDIR /app

EXPOSE 54321 9000

CMD ["./agent"]
