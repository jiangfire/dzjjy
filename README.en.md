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
- Idle suspend: `lifecycle.idleTimeout` suspends the app and wakes it on the next request.
- Temporary URLs: `exposure` publishes a static site behind an unguessable token path.
- Container controls: port mapping, secret injection, named volumes, resource limits.
- Sandbox isolation: optional gVisor `runsc`; it fails loudly instead of degrading.
- Kubernetes import: `import-k8s` converts a Deployment/Service into `dzjjy/v1`.

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

- `deploy` (legacy multipart deploy)
- `apply` (declarative YAML deploy)
- `validate` (local YAML validation, no server required)
- `import-k8s` (convert a Kubernetes Deployment/Service into `dzjjy/v1`)
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

### Phase 3 capabilities

```yaml
apiVersion: dzjjy/v1
kind: App
metadata:
  name: demo-api
spec:
  artifact: app.tar.gz
  runtime: {type: python, version: "3.12"}
  command: [python, "app.py", "--port", "9000"]
  sandbox: {type: docker, isolator: runsc}   # optional gVisor layer
  network:
    ports:
      - {container: 9000}                    # host port assigned when omitted
  secrets:
    - {key: "web/api-key", env: API_KEY}     # value comes from the server env
  volumes:
    - {name: demo-cache, path: /cache}       # named volumes only
  resources:
    memory: 512m
    cpus: "1.5"
    pids: 256
  lifecycle:
    ttl: 24h
    idleTimeout: 30m                         # suspend after 30m idle
```

`StaticSite` can be published behind an unguessable token path:

```yaml
apiVersion: dzjjy/v1
kind: StaticSite
metadata:
  name: demo-site
spec:
  artifact: dist.zip
  port: 0                 # auto-assigned
  exposure: {}            # random token; pin it with exposure.token
  lifecycle:
    idleTimeout: 10m
```

After a successful apply, `data.public_path` holds the public path (`/_dzjjy/<token>/`). The token is persisted, so it survives a server restart.

Secrets declare only the key; the value is read from the server process environment. A missing key fails the apply instead of silently starting a container without the variable.

Kubernetes import:

```bash
./build/dzjjy-client import-k8s -file k8s/deployment.yaml -out app.yaml
```

The converter accepts only maintained runtime images and single-container pods; anything it cannot represent is an error, and anything it can only degrade is reported as a note.

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

The current GitHub Actions baseline runs CI / Release / Quality workflows with Go `1.26.9`.

Related docs:

- `docs/ARCHITECTURE.md`
- `docs/DEVELOPMENT.md`
- `docs/TESTING.md`
- `docs/RELEASE.md`

## Notes

- This project is optimized for development/testing, not a full production PaaS.
- Use strong tokens and avoid exposing the service directly to public networks.
- Deployment overwrites app work directories; back up critical files first.
- When using `-state`, ensure the state file path is writable.

## License

This project is licensed under [AGPL-3.0](LICENSE).
