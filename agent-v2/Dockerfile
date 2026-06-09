FROM golang:1.24-alpine AS agent-builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /agent ./cmd/agent

FROM golang:1.24-alpine AS tcping-builder

ARG TCPING_REF=v2.8.0

RUN apk add --no-cache git bash make

RUN set -e; \
    git clone --depth 1 --branch "${TCPING_REF}" https://github.com/pouriyajamshidi/tcping.git /tmp/tcping && \
    cd /tmp/tcping && \
    make build

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    bird2 \
    wireguard-tools \
    iproute2 \
    iputils-ping \
    traceroute \
    vnstat \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=agent-builder /agent /app/agent
COPY --from=tcping-builder /tmp/tcping/target/tcping /usr/local/bin/tcping
RUN chmod +x /usr/local/bin/tcping

COPY config.example.yaml /app/config.yaml

WORKDIR /app

EXPOSE 54321

CMD ["./agent"]
