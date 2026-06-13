<p align="center">
  <img src="https://img.shields.io/badge/go-1.25-00ADD8?style=flat-square&logo=go" alt="Go 1.25" />
  <img src="https://img.shields.io/badge/react-19-61DAFB?style=flat-square&logo=react" alt="React 19" />
  <img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="MIT" />
  <img src="https://img.shields.io/badge/postgres-optional-blue?style=flat-square&logo=postgresql" alt="PostgreSQL optional" />
</p>

<h1 align="center">📷 Seym's Gallery</h1>

<p align="center">
  <em>A self-hosted photo gallery. Point it at a folder, and it just works.</em>
</p>

<p align="center">
  <a href="./README.zh-CN.md">🇨🇳 中文版</a>
</p>

---

## ✨ Features

- **🖥️ Zero config** — set `imageRoot` and start browsing instantly
- **📂 File-system aware** — directories become albums, nested hierarchy preserved
- **🖼️ Smart previews** — auto thumbnails, embedded JPEG extraction from RAW files
- **📸 EXIF parsing** — camera, lens, aperture, ISO, focal length, shutter count, star ratings
- **🎨 Retro skeuomorphic UI** — warm gray tones, tactile cards, dark/light mode, mobile responsive
- **📱 WeChat Moments timeline** — browse albums as a social feed with inline Markdown readmes
- **🔒 Album passwords** — drop an `ALBUM.yaml` with a password, no login system needed
- **👍 Anonymous stats** — device fingerprinting tracks views/likes without cookies or accounts
- **🌍 i18n** — English / 中文, auto-detected from browser
- **⚡ Performant** — LRU thumbnail cache, ETag-based HTTP caching, lazy loading

## 🚀 Quick Start

```bash
# Generate sample gallery for testing
make sample-gallery

# Install dependencies
make setup

# Start both frontend & backend
make dev
```

Open **http://127.0.0.1:5173** — backend listens on `127.0.0.1:8080`.

### Manual start

```bash
# Terminal 1 — backend
cd backend && go run . --config ../config.example.yaml

# Terminal 2 — frontend
cd frontend && npm run dev
```

Custom backend URL:

```bash
cd frontend && VITE_API_BASE=http://127.0.0.1:8080 npm run dev
```

## 📁 Project Structure

```
├── backend/            # Go server — API, thumbnails, EXIF, stats
│   ├── main.go         # Single binary, <2500 lines
│   ├── main_test.go
│   └── .air.toml       # Hot reload during development
├── frontend/           # React SPA — Vite + TypeScript
│   └── src/
│       ├── App.tsx     # Main component & all UI
│       ├── api.ts      # POST API client with deviceId
│       ├── deviceId.ts # FingerprintJS anonymous ID
│       ├── consent.ts  # EU cookie / copyright consent
│       ├── password.ts # Album password tokens
│       └── reactions.ts# Client-side like/dislike state
├── tools/              # Utility scripts
│   └── make-sample-gallery.go
├── config.example.yaml # Reference configuration
└── Makefile            # Top-level dev commands
```

## ⚙️ Configuration

Copy from example:

```bash
cp config.example.yaml config.yaml
```

Minimum config:

```yaml
imageRoot: /path/to/your/photos
```

### Stats Backend

| Backend | Use case |
|---------|----------|
| `memory` (default) | Single instance, dev/demo |
| `postgres` | Persistence, multi-instance |

```yaml
stats:
  backend: postgres
  postgres:
    dsn: "postgres://user:pass@localhost:5432/gallery?sslmode=disable"
```

Tables are auto-created on startup — no migrations needed.

### Album Passwords

Drop an `ALBUM.yaml` in any album directory:

```yaml
password:
  value: "mysecret"
  hint: "Our wedding date"   # optional
readme: |
  ## Summer Trip 2024
  
  Best memories from the coast.
```

- `readme` in YAML takes priority over `README.md`
- Passwords verified server-side, token stored in `sessionStorage`

## 📡 API

All business endpoints are `POST` with JSON body:

| Endpoint | Purpose |
|----------|---------|
| `/api/list-albums` | Album tree with stats |
| `/api/list-images` | Photos in an album |
| `/api/get-image-detail` | Full EXIF + metadata |
| `/api/get-status` | Server health & config |
| `/api/record-view` | Record a page view |
| `/api/react-item` | Like / dislike |
| `/api/verify-album-password` | Unlock album |

Media endpoints (GET):

| Endpoint | Response |
|----------|----------|
| `/media/thumb/{id}` | JPEG thumbnail |
| `/media/original/{id}` | Full-resolution image |
| `/media/raw/{id}` | RAW file download |

Response format:

```json
{ "ok": true, "data": {} }
```

## 🔒 Privacy

- **No accounts** — device fingerprinting via [FingerprintJS](https://github.com/fingerprintjs/fingerprintjs)
- **No cookies** — consent and preferences stored in `localStorage`
- **No tracking** — stats are counts only, never tied to identity
- **EU compliant** — cookie consent banner on first visit

## 🛠️ Tech Stack

| Layer | Technology |
|-------|------------|
| Backend | Go, `net/http`, `pgx/v5` |
| Frontend | React 19, TypeScript, Vite |
| Styling | CSS custom properties, dark/light theme |
| Images | `golang.org/x/image`, EXIF via `goexif` |
| Config | YAML |
| Stats | In-memory LRU or PostgreSQL |

## 📄 License

MIT © 2026 [zsh2401](https://github.com/zsh2401)

---

<p align="center">
  <sub>Built for photographers who want to own their work.</sub>
</p>
