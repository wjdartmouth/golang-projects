# Go × 日本 — Japanese Market Go Portfolio

Seven self-contained Go HTTP services for real Japanese-domain problems:
national holidays, qualified invoices (インボイス制度), Japan Post lookup,
JLPT vocabulary quizzes, labor-law attendance (労働基準法), e-commerce
price tracking, and furigana injection middleware.

Each service is a stdlib-first `net/http` app (Go 1.21), exposes a JSON
REST API and `/health`, and can be run with `go run .` or Docker.
Live overview: https://wjdartmouth.github.io/golang-projects/

This is a personal portfolio collection, not a single deployable
microservices platform. Data is in-memory unless noted.

[![Go Version](https://img.shields.io/badge/Go-1.21-00AED8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-gold)](./LICENSE)
[![Services](https://img.shields.io/badge/Services-7-crimson)](#projects)
[![CGO](https://img.shields.io/badge/CGO-free-brightgreen)](#tech-stack)

---

## Table of Contents

- [Projects](#projects)
- [Repository Structure](#repository-structure)
- [Quick Start](#quick-start)
- [Tech Stack](#tech-stack)
- [Design Principles](#design-principles)
- [License](#license)

---

## Projects

| # | Service | Folder | Port | Deps | Docs |
|---|---------|--------|------|------|------|
| 01 | 祝日 API | [holidays-api](./holidays-api/) | 8080 | stdlib | [README](./holidays-api/README.md) |
| 02 | 請求書 API | [invoice-gen](./invoice-gen/) | 8081 | stdlib | [README](./invoice-gen/README.md) |
| 03 | 郵便番号 API | [postal-api](./postal-api/) | 8082 | stdlib | [README](./postal-api/README.md) |
| 04 | JLPT 語彙クイズ | [jlpt-quiz-api](./jlpt-quiz-api/) | 8083 | stdlib | [README](./jlpt-quiz-api/README.md) · [demo](./jlpt-quiz-api/demo.html) |
| 05 | 勤怠管理 API | [kintai-api](./kintai-api/) | 8084 | stdlib | [README](./kintai-api/README.md) · [dashboard](./kintai-api/dashboard.html) |
| 06 | 価格追跡 | [price-tracker](./price-tracker/) | 8085 | x/net, x/text | [README](./price-tracker/README.md) · [dashboard](./price-tracker/dashboard.html) |
| 07 | 振仮名 MW | [furigana](./furigana/) | 8086 | x/net | [README](./furigana/README.md) · [demo](./furigana/demo.html) |

### 01 — 祝日 API (Japanese Holidays)

Computes Japan's national holiday calendar from **国民の祝日に関する法律**, not a static table: fixed dates, Happy Monday rules, equinox approximations, and 振替休日 chaining. Check whether a date is a business day, or ask for the next working day after weekends and holidays.

See [holidays-api/README.md](./holidays-api/README.md) for endpoints. Run with `cd holidays-api && go run .` (port 8080).

### 02 — 請求書 API (Invoice Generator)

Builds Japan-compliant invoices with **インボイス制度** support: 10% standard and 8% reduced consumption tax, per-line itemization, registration-number display, and tax-rate subtotals. Returns JSON or print-ready HTML (Noto Sans JP, `@media print`).

See [invoice-gen/README.md](./invoice-gen/README.md) for the request body and endpoints. Run with `cd invoice-gen && go run .` (port 8081), then open `/api/v1/invoice/preview`.

### 03 — 郵便番号 API (Postal Code Lookup)

Looks up addresses by Japan Post postal code. Accepts hyphenated or 7-digit codes, searches address and kana fields, and protects the in-memory map with `sync.RWMutex`. Starts with 15 built-in sample records when `KEN_ALL.CSV` is not present; set `POSTAL_CSV` to load the official dataset.

See [postal-api/README.md](./postal-api/README.md) for endpoints and CSV setup. Run with `cd postal-api && go run .` (port 8082).

### 04 — JLPT 語彙クイズ (Vocabulary Quiz API)

A quiz engine for JLPT N5–N1 with 104 vocabulary items across 14 categories, four modes (kanji→meaning, meaning→kanji, kana→kanji, flashcard), streak scoring, and a wrong-word review list. `demo.html` can be opened in a browser without the server.

See [jlpt-quiz-api/README.md](./jlpt-quiz-api/README.md) for the quiz flow. Run with `cd jlpt-quiz-api && go run .` (port 8083).

### 05 — 勤怠管理 API (Attendance Management)

Attendance, overtime, and paid-leave workflows aligned with **労働基準法** articles 32, 34, 37, and 39, plus 36協定-style overtime alerts. Includes clock-in/out, leave approval, monthly summaries, and a 7-tab `dashboard.html`.

See [kintai-api/README.md](./kintai-api/README.md) for endpoints and the tenure table. Run with `cd kintai-api && go run .` (port 8084).

### 06 — 価格追跡 (Price Tracker)

Monitors product prices on 楽天市場, Yahoo!ショッピング, Amazon.co.jp, メルカリ, and au PAYマーケット. Handles Shift-JIS / EUC-JP pages, polite delays, target-price / drop / all-time-low / back-in-stock alerts, and a CLI scrape mode. Also available as a [standalone repository](https://github.com/wjdartmouth/price-tracker); the copy in this collection lives at `price-tracker/`.

See [price-tracker/README.md](./price-tracker/README.md) for API, CLI, and environment variables. Run with `cd price-tracker && go run .` (port 8085, overridable with `PORT`).

### 07 — 振仮名ミドルウェア (Furigana Injection)

HTTP middleware and a small demo server that wrap kanji in HTML with `<ruby>` annotations. Ships with a built-in dictionary and a pluggable `Annotator` interface (MeCab or Kuromoji can be swapped in). The code is `package main` in one file — copy the types into your own service, or run this directory as a server; it is not a separately importable library module.

See [furigana/README.md](./furigana/README.md) for API, CLI, and skip rules. Start the server with `cd furigana && go run . serve` (port 8086, overridable with `PORT`).

---

## Repository Structure

```
golang-projects/
│
├── holidays-api/          # 01 — 祝日 API
│   ├── main.go
│   ├── go.mod
│   ├── Dockerfile
│   └── README.md
│
├── invoice-gen/           # 02 — 請求書 API
│   ├── main.go
│   ├── go.mod
│   ├── Dockerfile
│   └── README.md
│
├── postal-api/            # 03 — 郵便番号 API
│   ├── main.go
│   ├── go.mod
│   ├── Dockerfile
│   └── README.md
│
├── jlpt-quiz-api/         # 04 — JLPT 語彙クイズ
│   ├── main.go
│   ├── demo.html
│   ├── go.mod
│   ├── Dockerfile
│   └── README.md
│
├── kintai-api/            # 05 — 勤怠管理 API
│   ├── main.go
│   ├── dashboard.html
│   ├── go.mod
│   ├── Dockerfile
│   └── README.md
│
├── price-tracker/         # 06 — 価格追跡サービス
│   ├── main.go
│   ├── dashboard.html
│   ├── go.mod
│   ├── go.sum
│   ├── Dockerfile
│   └── README.md
│
├── furigana/              # 07 — 振仮名ミドルウェア
│   ├── main.go
│   ├── demo.html
│   ├── go.mod
│   ├── go.sum
│   ├── Dockerfile
│   └── README.md
│
├── docker-compose.yml     # Starts all 7 services
├── index.html             # GitHub Pages overview
├── .gitattributes
├── .gitignore
├── README.md              # This file
└── LICENSE
```

---

## Quick Start

### Run everything with Docker Compose

```bash
git clone https://github.com/wjdartmouth/golang-projects.git
cd golang-projects
docker compose up --build
```

All seven services start on consecutive ports:

| Service | Port | Path |
|---------|------|------|
| 祝日 API | 8080 | `http://localhost:8080` |
| 請求書 API | 8081 | `http://localhost:8081` |
| 郵便番号 API | 8082 | `http://localhost:8082` |
| JLPT クイズ | 8083 | `http://localhost:8083` |
| 勤怠管理 API | 8084 | `http://localhost:8084` |
| 価格追跡 | 8085 | `http://localhost:8085` |
| 振仮名 MW | 8086 | `http://localhost:8086` |

### Run a single service

```bash
# Most services
cd holidays-api
go run .

# Furigana needs the serve subcommand
cd furigana
go run . serve
```

`price-tracker` and `furigana` pull `golang.org/x/net` (and `x/text` for the tracker). Run `go mod tidy` in those folders if your local module cache is empty.

### Health checks

```bash
for port in 8080 8081 8082 8083 8084 8085 8086; do
  echo -n "Port $port: "
  curl -s "http://localhost:$port/health" | jq -r '.status // .service'
done
```

---

## Tech Stack

| Component | Detail |
|-----------|--------|
| **Language** | Go 1.21 |
| **HTTP** | `net/http` standard library — no web framework |
| **JSON** | `encoding/json` standard library |
| **Concurrency** | `sync.RWMutex` in-memory stores across all services |
| **HTML parsing** | `golang.org/x/net/html` (furigana middleware, price tracker) |
| **Charset decoding** | `golang.org/x/text` — Shift-JIS, EUC-JP (price tracker) |
| **Containers** | Multi-stage Docker builds; scratch or alpine base |
| **CGO** | None — all services compile with `CGO_ENABLED=0` |
| **Docs** | Bilingual README (Japanese + English) in every project folder |

External dependencies are intentionally minimal. Five of the seven services use only the Go standard library. The two exceptions (`price-tracker` and `furigana`) add `golang.org/x/net` and `golang.org/x/text` from the official Go extended packages — these are the only non-stdlib imports in the entire portfolio.

---

## Design Principles

**Stdlib first.** The default answer to "should I add a package?" is no. Every external dependency is a deployment constraint, a security surface, and a maintenance burden. This portfolio shows that HTTP routing, JSON serialization, concurrency, and HTML parsing are available in the Go standard library without a framework.

**Japanese law as code.** Three projects implement Japanese legal requirements as executable logic rather than configuration: the attendance API encodes 労働基準法 articles 32, 34, 37, and 39 as calculation functions; the invoice generator itemizes 10% and 8% rates for the 2023 インボイス制度; the holiday API derives public holiday dates from 国民の祝日に関する法律. The goal is for the code to read as a faithful translation of the law.

**Interface-driven extensibility.** The furigana middleware's `Annotator` interface lets you substitute MeCab, Kuromoji, or a cloud NLP API without touching the HTTP layer. The price tracker's platform scrapers share a common `ScrapeProduct` dispatcher. New platforms and backends can slot in without modifying the core.

**Encoding awareness.** Legacy Japanese systems frequently produce Shift-JIS and EUC-JP output. The price tracker detects the charset from the `Content-Type` response header and decodes it before HTML parsing — a real-world problem that most scraping tutorials skip over.

**Concurrency safety.** Every in-memory store uses `sync.RWMutex` with explicit lock discipline: read operations hold `RLock`, write operations hold the full `Lock`. The postal code API is designed for concurrent lookups on a single instance without data races.

**Bilingual documentation.** Every project has a Japanese + English README, and API field names and error messages are written to be readable by both Japanese- and English-speaking developers.

---

## License

MIT License — see [LICENSE](./LICENSE) for details.

---

*Built as a portfolio targeting the Japanese software engineering market.*  
*Published on [Zenn.dev](https://zenn.dev/jian_wang) · [Qiita](https://qiita.com/wjdartmouth) · [GitHub](https://github.com/wjdartmouth)*
