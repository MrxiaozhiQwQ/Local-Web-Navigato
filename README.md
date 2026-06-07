# Local Web Navigator

[简体中文说明](./README.zh-CN.md)

Local Web Navigator is a lightweight web app that scans a target host for web services and turns the discovered pages into a navigation homepage.

It automatically checks common web ports, falls back to a wider port scan, extracts page titles and favicons, stores discovered sites locally, and removes sites that are no longer available.

## Features

- Scan a target host for web services
- Prioritize previously discovered ports before scanning other ports
- Extract page titles and favicons automatically
- Show only discovered sites on the homepage
- Change scan target from the settings panel
- Show a compact floating scan progress bar at the bottom
- Auto-hide the progress bar when scanning completes
- Persist discovered sites and settings locally
- Remove offline sites automatically
- Support Linux Docker deployment
- Support GitHub Releases auto-build

## Tech Stack

- Backend: Go
- Frontend: HTML, CSS, JavaScript
- Realtime updates: SSE
- Storage: local JSON files

## Project Structure

```text
.
├─ main.go
├─ public/
│  ├─ index.html
│  ├─ app.js
│  └─ styles.css
├─ data/
├─ Dockerfile
├─ docker-compose.yml
├─ .github/
│  └─ workflows/
│     └─ release.yml
├─ README.md
└─ LICENSE
```

## Local Run

### Requirements

- Go 1.25+

### Build

Windows PowerShell:

```powershell
$env:GOCACHE="$PWD\.gocache"
& "C:\Program Files\Go\bin\go.exe" build -o local-web-nav.exe .
```

Linux / macOS:

```bash
go build -o local-web-nav .
```

### Start

Windows:

```powershell
.\local-web-nav.exe
```

Linux / macOS:

```bash
./local-web-nav
```

Default URL:

```text
http://127.0.0.1:3210
```

## Environment Variables

- `PORT`
  Server port, default: `3210`
- `DATA_DIR`
  Data directory, default: `./data`

Example:

```bash
PORT=8080 DATA_DIR=./data ./local-web-nav
```

## Data Files

The app stores runtime data in `DATA_DIR`:

- `sites.json`
  Discovered site history
- `settings.json`
  Current scan target

## Docker Deployment

### Linux Docker with compose

Recommended for Linux hosts:

```bash
docker compose up -d --build
```

Then open:

```text
http://SERVER_IP:3210
```

### Linux Docker with docker run

```bash
docker build -t local-web-nav .

docker run -d \
  --name local-web-nav \
  --restart unless-stopped \
  --network host \
  -e PORT=3210 \
  -e DATA_DIR=/data \
  -v $(pwd)/data:/data \
  local-web-nav
```

## Why `host` Network is Recommended on Linux

This project scans ports and web services on the target host.

With Docker bridge networking, `127.0.0.1` and `localhost` usually point to the container itself instead of the host. On Linux, `network_mode: host` makes scanning behavior much closer to the real host network, so it fits this project better.

## GitHub Releases Auto Build

This repository includes a GitHub Actions workflow:

`/.github/workflows/release.yml`

It automatically:

- builds release binaries when a tag like `v1.0.0` is pushed
- creates a GitHub Release
- uploads compiled binaries for multiple platforms

### Included targets

- Linux amd64
- Linux arm64
- Windows amd64
- macOS amd64
- macOS arm64

### How to trigger a release

1. Commit and push your latest code.
2. Create a version tag locally:

```bash
git tag v1.0.0
git push origin v1.0.0
```

3. GitHub Actions will automatically build and publish the release.

### Release files

Generated assets use names like:

- `local-web-nav_linux_amd64.tar.gz`
- `local-web-nav_linux_arm64.tar.gz`
- `local-web-nav_windows_amd64.zip`
- `local-web-nav_darwin_amd64.tar.gz`
- `local-web-nav_darwin_arm64.tar.gz`

## Usage

1. Open the homepage
2. The app starts scanning automatically
3. The homepage shows only discovered web pages
4. Click the settings button in the top-right corner
5. Change the scan target, for example:
   - `192.168.1.10`
   - `localhost`
   - `127.0.0.1`
6. Click `Apply and Scan`

## Scan Logic

Scan order:

1. Previously discovered ports
2. Common web ports
3. Remaining ports

Page detection:

- `Content-Type` contains `text/html` or `application/xhtml+xml`
- or the response body contains `<html`, `<title`, or `<!doctype html`

Page naming:

1. `<title>`
2. `Server` header
3. `IP:port`

## Notes

- Scan only hosts and networks you are authorized to manage
- Full port scans can take time
- First scan is slower than later scans because history is empty
- In Docker, scanning `localhost` behaves best on Linux with `host` networking


## License

MIT
