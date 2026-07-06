# DN42 Bot Agent Documentation

This directory contains documentation for the DN42 bot agent (agent v2, written in Go).

## Overview

The DN42 bot agent runs on each peering node. It manages WireGuard tunnels, BIRD peering configs, and responds to API requests from the Telegram bot server. The agent handles peer creation, removal, route lookups, network diagnostics, and traffic monitoring.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                 Telegram Bot Server              │
│                                                  │
│  ┌──────────┐  ┌──────────┐  ┌───────────────┐  │
│  │  Bot API  │  │  Peering │  │  Tool Commands │  │
│  │  Handler  │  │  Manager │  │  (ping, etc)  │  │
│  └────┬─────┘  └────┬─────┘  └──────┬────────┘  │
│       └──────────────┼───────────────┘           │
│                      │ HTTP API                  │
└──────────────────────┼───────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────┐
│               Agent (Go Binary)                   │
│                                                   │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  │
│  │   chi HTTP │  │  WireGuard │  │   BIRD     │  │
│  │   Router   │  │  Manager   │  │  Manager   │  │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  │
│        │               │               │          │
│  ┌─────┴──────┐  ┌─────┴──────┐  ┌─────┴──────┐  │
│  │  Auth MW   │  │  /etc/     │  │  bird.ctl  │  │
│  │  Sentry MW │  │  wireguard │  │  socket    │  │
│  └────────────┘  └────────────┘  └────────────┘  │
└──────────────────────────────────────────────────┘
        │               │               │
        ▼               ▼               ▼
  ┌──────────┐   ┌──────────┐   ┌──────────┐
  │  Network  │   │  Tun/    │   │  vnstat  │
  │  Stack    │   │  WireG.  │   │  monitor │
  └──────────┘   └──────────┘   └──────────┘
```

## Key Components

- **chi HTTP Router** — handles all API endpoints from the server
- **WireGuard Manager** — creates, configures, and tears down WireGuard tunnels
- **BIRD Manager** — writes peering config snippets and queries BIRD for routes
- **Auth Middleware** — validates the shared secret on protected endpoints
- **Sentry Middleware** — captures errors and sends them to Sentry (optional)

## Documentation

| Document | Contents |
|----------|----------|
| [api-reference.md](api-reference.md) | HTTP API endpoints, request/response formats, and status codes |
| [config.md](config.md) | Full config reference with all YAML keys, types, defaults, and conditional logic |
| [deployment.md](deployment.md) | Docker build, Docker Compose, volume mounts, capabilities, and systemd setup |
| [migration.md](migration.md) | Migration guide from the Python agent (v1) to the Go agent (v2) |

## Quick Start

```bash
# 1. Create config
cp agent-v2/config.example.yaml config.yaml
# Edit config.yaml with your settings

# 2. Run with Docker
docker compose up -d

# 3. Check logs
docker compose logs -f
```

## Project Structure

```
agent-v2/
├── cmd/agent/main.go        # Entry point, HTTP server setup
├── internal/
│   ├── config/              # YAML config loading and validation
│   ├── handler/             # HTTP endpoint handlers
│   ├── middleware/           # Auth and Sentry middleware
│   └── service/             # Business logic (WireGuard, BIRD, diagnostics)
├── config.example.yaml      # Example configuration
├── Dockerfile               # Multi-stage Docker build
├── go.mod                   # Go module definition
└── go.sum                   # Dependency checksums
```
