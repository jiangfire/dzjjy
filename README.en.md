# dzjjy

[中文](README.md) | [English](README.en.md)

[![CI](https://github.com/jiangfire/dzjjy/actions/workflows/ci.yml/badge.svg)](https://github.com/jiangfire/dzjjy/actions/workflows/ci.yml)
[![Quality](https://github.com/jiangfire/dzjjy/actions/workflows/quality.yml/badge.svg)](https://github.com/jiangfire/dzjjy/actions/workflows/quality.yml)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)](https://go.dev/)

A lightweight deployment and process management tool for development/testing environments.

`dzjjy` provides a simple server + client workflow for:
uploading apps, starting processes, checking status/logs, restarting/stopping, and managing multiple apps.

## Contents

- [Positioning](#positioning)
- [Key Features](#key-features)
- [Quick Start](#quick-start)
- [Usage Examples](#usage-examples)
- [API Overview](#api-overview)
- [Development & Testing](#development--testing)
- [Notes](#notes)
- [License](#license)

## Positioning

Best for:

- Running multiple local services during development
- Managing app lifecycle in microservice integration testing
- Fast deployment steps in CI/testing environments

Goals:

- Low usage cost (simple commands, minimal dependencies)
- Multi-app management (isolated work dir, state, and logs per app)
- Basic resilience (auto-restart + state persistence)

## Key Features

- Multi-app management by app name.
- Flexible deployment: single file or archives (`.zip`, `.tar`, `.tar.gz`, `.gz`).
- Process guard: `auto-restart` with max restart limits.
- Log query with `lines`; the client also supports `--follow`.
- Optional state persistence via `-state`, with metadata restoration and configured auto-restart.
- Basic security controls: Bearer token auth, path/input validation.

## Quick Start

### 1. Build

```bash
git clone https://github.com/jiangfire/dzjjy.git
cd dzjjy
make deps
make build
```

Build outputs:

- `build/dzjjy-server`
- `build/dzjjy-client`

On Windows, use the `.exe` binaries.

### 2. Start server

```bash
./build/dzjjy-server \
  -token your-secret-token \
  -port 8080 \
  -upload ./uploads \
  -work ./workspace \
  -log ./logs \
  -state ./state.json
```

Arguments:

- `-token`: auth token (required)
- `-port`: server port (default `8080`)
- `-upload`: upload staging dir (default `./uploads`, keeps the latest uploaded artifact per app)
- `-work`: app work dir (default `./workspace`)
- `-log`: log dir (default `./logs`)
- `-state`: state file (optional, recommended)

Health check:

```text
GET /health
```

### 3. Client commands

```bash
./build/dzjjy-client <command> [options]
```

Available commands:

- `deploy`
- `start`
- `stop`
- `restart`
- `status`
- `logs`
- `list`
- `remove`

## Usage Examples

### Deploy runtime app (e.g. Python)

```bash
./build/dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -app demo-python \
  -file app.zip \
  -type runtime \
  -executable python3 \
  -entry app.py \
  -args "--port 9000" \
  -auto-restart \
  -max-restarts 5
```

### Deploy exec app (e.g. Go binary)

```bash
./build/dzjjy-client deploy \
  -server http://localhost:8080 \
  -token your-secret-token \
  -app demo-go \
  -file demo-go.exe \
  -type exec \
  -executable demo-go.exe \
  -auto-restart
```

### Common operations

```bash
# list all apps
./build/dzjjy-client list -server http://localhost:8080 -token your-secret-token

# app status
./build/dzjjy-client status -server http://localhost:8080 -token your-secret-token -app demo-python

# last 200 log lines
./build/dzjjy-client logs -server http://localhost:8080 -token your-secret-token -app demo-python -lines 200

# restart app
./build/dzjjy-client restart -server http://localhost:8080 -token your-secret-token -app demo-python

# stop app
./build/dzjjy-client stop -server http://localhost:8080 -token your-secret-token -app demo-python
```

If `-app` is omitted, the default app name is `default`.

## API Overview

Auth header:

```text
Authorization: Bearer <token>
```

Multi-app endpoints:

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/apps` | List apps |
| POST | `/api/v1/apps/{name}/deploy` | Deploy app |
| POST | `/api/v1/apps/{name}/start` | Start app |
| POST | `/api/v1/apps/{name}/stop` | Stop app |
| POST | `/api/v1/apps/{name}/restart` | Restart app |
| GET | `/api/v1/apps/{name}/status` | App status |
| GET | `/api/v1/apps/{name}/logs?lines=N` | App logs |
| DELETE | `/api/v1/apps/{name}/remove` | Remove app |

Compatible delete path:

- `DELETE /api/v1/apps/{name}`

Legacy single-app endpoints (`default`):

- `POST /api/v1/deploy`
- `POST /api/v1/start`
- `POST /api/v1/stop`
- `POST /api/v1/restart`
- `GET /api/v1/status`
- `GET /api/v1/logs`

## Development & Testing

```bash
make deps        # dependencies
make build       # build binaries
make test        # unit tests
make lint        # static checks
make fmt         # format code
make ci          # local CI pipeline
```

Related docs:

- `docs/ARCHITECTURE.md`
- `docs/DEVELOPMENT.md`
- `docs/TESTING.md`
- `docs/PLAN.md`
- `docs/RELEASE.md`

## Notes

- This project is optimized for development/testing, not a full production PaaS.
- Use strong tokens and avoid exposing the service directly to public networks.
- Deployment overwrites app work directories; back up critical files first.
- When using `-state`, ensure the state file path is writable.

## License

This project is licensed under [AGPL-3.0](LICENSE).
