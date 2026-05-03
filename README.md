# Go × 日本 — Japanese Market Microservices Portfolio

> A collection of seven production-grade Go microservices built for the Japanese technology market. Each project targets a real domain — national holidays, invoice compliance, postal infrastructure, quiz systems, attendance law, e-commerce intelligence, and language tooling. All services expose JSON REST APIs, include bilingual documentation, and ship in minimal Docker containers. Zero CGO dependencies where possible.

[![Go Version](https://img.shields.io/badge/Go-1.21-00AED8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-gold)](./LICENSE)
[![Services](https://img.shields.io/badge/Services-7-crimson)](#projects)
[![CGO](https://img.shields.io/badge/CGO-free-brightgreen)](#tech-stack)

---

## Table of Contents

- [Projects](#projects)
  - [01 — 祝日 API](#01--祝日-api-japanese-holidays-microservice)
  - [02 — 請求書 API](#02--請求書-api-invoice-generator)
  - [03 — 郵便番号 API](#03--郵便番号-api-postal-code-lookup)
  - [04 — JLPT 語彙クイズ](#04--jlpt-語彙クイズ-vocabulary-quiz-api)
  - [05 — 勤怠管理 API](#05--勤怠管理-api-attendance-management)
  - [06 — 価格追跡サービス](#06--価格追跡サービス-price-tracker)
  - [07 — 振仮名ミドルウェア](#07--振仮名ミドルウェア-furigana-injection-middleware)
- [Repository Structure](#repository-structure)
- [Quick Start](#quick-start)
- [Tech Stack](#tech-stack)
- [Design Principles](#design-principles)
- [License](#license)

---

## Projects

### 01 — 祝日 API (Japanese Holidays Microservice)

**Port:** `8080` | **Path:** `./holidays-api/` | **Deps:** stdlib only

Implements Japan's complete national holiday calendar per **国民の祝日に関する法律** — in Go logic, not a static database. Covers all 16 holidays including the four Happy Monday rules, 振替休日 (substitute holiday) chaining through consecutive days, and the spring and autumn equinox approximated via orbital mechanics formulas.

**Endpoints**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/holidays?year=2025` | All holidays for a given year |
| `GET` | `/api/v1/check?date=2025-01-01` | Check whether a date is a holiday |
| `GET` | `/api/v1/next-business-day?from=2025-04-30` | Next working day (skips weekends and holidays) |

**Notable implementation details**

- Happy Monday calculation for 成人の日, 海の日, 敬老の日, and 体育の日 (second or third Monday of the designated month)
- 振替休日 cascade — if a holiday falls on Sunday and the following Monday is *also* a holiday, the substitute shifts to Tuesday
- Equinox dates computed via the Oudin/Meeus approximation rather than a hardcoded lookup table
- `GET /api/v1/holidays` returns machine-readable JSON with Japanese names, English names, and law article references

```bash
curl "http://localhost:8080/api/v1/holidays?year=2025"
# → {"year":2025,"count":21,"holidays":[{"name":"元日","name_en":"New Year's Day","date":"2025-01-01","law_article":"第2条第1号"},...]}

curl "http://localhost:8080/api/v1/next-business-day?from=2025-04-28"
# → {"from":"2025-04-28","next_business_day":"2025-05-07","skipped_days":9}
```

```bash
# Run
cd holidays-api && go run main.go
```

---

### 02 — 請求書 API (Invoice Generator)

**Port:** `8081` | **Path:** `./invoice-gen/` | **Deps:** stdlib only

Generates Japan-compliant invoices with full **インボイス制度** (qualified invoice system, effective October 2023) support. Handles the 10% standard and 8% reduced consumption tax rates with per-line itemization, T-number display, and both HTML and JSON output formats.

**Endpoints**

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/invoice/generate` | Generate invoice (JSON response) |
| `POST` | `/api/v1/invoice/generate?format=html` | Generate invoice (HTML response, print-ready) |
| `GET` | `/api/v1/invoice/preview` | Demo invoice with sample data |

**Request body**

```json
{
  "invoice_number": "INV-2025-001",
  "issue_date": "2025-05-01",
  "due_date": "2025-05-31",
  "seller": {
    "name": "株式会社サンプル",
    "t_number": "T1234567890123",
    "address": "東京都渋谷区..."
  },
  "buyer": { "name": "株式会社テスト" },
  "items": [
    {"description": "システム開発費", "quantity": 1, "unit_price": 500000, "tax_rate": 0.10},
    {"description": "食料品（軽減税率対象）", "quantity": 10, "unit_price": 1000, "tax_rate": 0.08}
  ]
}
```

**Notable implementation details**

- Validates T-number format (T + 13 digits, Luhn-compatible check digit)
- Generates separate subtotals for 10% and 8% tax rates as required by インボイス制度
- HTML output uses a `@media print` CSS block for direct browser printing — no PDF library needed
- Noto Sans JP font loaded from Google Fonts for correct Japanese rendering

```bash
cd invoice-gen && go run main.go
# Open http://localhost:8081/api/v1/invoice/preview
```

---

### 03 — 郵便番号 API (Postal Code Lookup)

**Port:** `8082` | **Path:** `./postal-api/` | **Deps:** stdlib only

High-performance postal code lookup service backed by Japan Post's **KEN_ALL.CSV** dataset (124,000+ entries). Uses a `map[string]PostalRecord` for O(1) average-case lookups with `sync.RWMutex` protection for safe concurrent access. Ships with 15 built-in sample records for zero-config startup.

**Endpoints**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/postal/lookup?code=100-0001` | Exact postal code lookup |
| `GET` | `/api/v1/postal/search?q=千代田区` | Full-text search over address fields |
| `GET` | `/api/v1/postal/validate?code=1000001` | Validate format only (no DB lookup) |
| `GET` | `/api/v1/postal/stats` | Dictionary statistics |

**Environment variables**

| Variable | Default | Description |
|----------|---------|-------------|
| `POSTAL_CSV` | `./KEN_ALL.CSV` | Path to Japan Post CSV file |
| `PORT` | `8082` | Listen port |

**Notable implementation details**

- Accepts codes with or without hyphen (`100-0001` and `1000001` both resolve)
- Full-text search scans 都道府県, 市区町村, and 町域 fields with romaji support
- `sync.RWMutex` — reads take `RLock()` for concurrent throughput; write lock only on CSV reload
- KEN_ALL.CSV available from [Japan Post](https://www.post.japanpost.jp/zipcode/download.html) (free, updated monthly)

```bash
# With Japan Post dataset
POSTAL_CSV=./KEN_ALL.CSV go run ./postal-api/main.go

# Zero-config (built-in sample data)
go run ./postal-api/main.go

curl "http://localhost:8082/api/v1/postal/lookup?code=100-0001"
# → {"postal_code":"1000001","prefecture":"東京都","city":"千代田区","town":"千代田"}
```

---

### 04 — JLPT 語彙クイズ (Vocabulary Quiz API)

**Port:** `8083` | **Path:** `./jlpt-quiz-api/` | **Deps:** stdlib only

Quiz engine for JLPT N5–N1 vocabulary with session management, streak-based scoring multipliers, and a wrong-word review queue. Ships with 104 vocabulary items across 14 semantic categories, each with kanji, kana, romaji, English meaning, and example sentences. The standalone `demo.html` file runs entirely in the browser without a server.

**Endpoints**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/vocabulary` | Full vocabulary list (`?level=N5`, `?category=food`) |
| `GET` | `/api/v1/vocabulary/random` | Random word (`?level=N3`) |
| `POST` | `/api/v1/quiz/start` | Start quiz session |
| `POST` | `/api/v1/quiz/answer` | Submit answer, returns correctness + next question |
| `GET` | `/api/v1/quiz/:id/summary` | Session summary with grade and wrong-word list |
| `GET` | `/api/v1/levels` | JLPT level statistics |

**Quiz modes**

| Mode | Question | Answer |
|------|----------|--------|
| `kanji_to_meaning` | 漢字 | English meaning |
| `meaning_to_kanji` | English meaning | 漢字 |
| `kana_to_kanji` | ふりがな | 漢字 |
| `flashcard` | 漢字 (3D flip card, self-assessed) | — |

**Scoring**

- Base score: 10 points per correct answer
- **3× multiplier** triggers at 5 consecutive correct answers
- **2× multiplier** triggers at 3 consecutive correct answers
- Grades: **S** (95%+) · **A** (80%+) · **B** (65%+) · **C** (50%+) · **D** (<50%)
- Wrong-word queue surfaces missed items in subsequent sessions

```bash
cd jlpt-quiz-api && go run main.go

# Start a 10-question N4 quiz
curl -X POST http://localhost:8083/api/v1/quiz/start \
  -H 'Content-Type: application/json' \
  -d '{"mode":"kanji_to_meaning","level":"N4","question_count":10}'

# Or open demo.html directly in a browser (no server needed)
open demo.html
```

---

### 05 — 勤怠管理 API (Attendance Management)

**Port:** `8084` | **Path:** `./kintai-api/` | **Deps:** stdlib only

Labor law–compliant attendance management system for Japanese workplaces. Implements the relevant articles of **労働基準法** (Labor Standards Act) for overtime calculation, mandatory breaks, paid leave accrual, and 36協定 alert thresholds. Includes a full leave request approval workflow and a 7-tab interactive management dashboard.

**Labor law compliance**

| Article | Provision | Implementation |
|---------|-----------|----------------|
| 労基法第32条 | Legal working hours: 8h/day, 40h/week | Anything over 480 min/day → overtime |
| 労基法第34条 | Mandatory breaks: 45 min (>6h), 60 min (>8h) | Auto-deducted from work time |
| 労基法第37条 | Overtime premium: 1.25×; Late-night (22:00–05:00): 1.35× | Per-minute overlap calculation |
| 労基法第39条 | Paid leave accrual by tenure | Lookup table: 6mo=10d → 6.5yr+=20d |
| 36協定 | Overtime alert threshold: 45h/month | Generates labor alerts in `/reports` |

**Endpoints**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/employees` | Employee list (`?department=`) |
| `POST` | `/api/v1/employees` | Create employee |
| `POST` | `/api/v1/clock` | Clock in/out or break start/end |
| `GET` | `/api/v1/attendance` | Monthly attendance (`?employee_id=&month=`) |
| `PUT` | `/api/v1/attendance/:empID/:date` | Manual correction |
| `POST` | `/api/v1/leave` | Submit leave request |
| `POST` | `/api/v1/leave/:id/approve` | Approve or reject leave |
| `GET` | `/api/v1/leave/balance` | Paid leave balance |
| `GET` | `/api/v1/reports/monthly` | Monthly summary with wage estimate |
| `GET` | `/api/v1/reports/overtime` | Overtime breakdown per employee |
| `GET` | `/api/v1/stats` | Company-wide statistics |

**Clock-in request**

```bash
curl -X POST http://localhost:8084/api/v1/clock \
  -H 'Content-Type: application/json' \
  -d '{"employee_id":"emp-001","type":"clock_in","location":"office"}'
# type: clock_in | clock_out | break_start | break_end

# Monthly report
curl "http://localhost:8084/api/v1/reports/monthly?employee_id=emp-001&month=2025-05"
# → {"work_days":18,"overtime_minutes":420,"overtime_pay":4375,...,"alerts":["⚡ 月間残業7.0h"]}

# Open dashboard
open dashboard.html
```

---

### 06 — 価格追跡サービス (Price Tracker)

**Port:** `8085` | **Path:** `./price-tracker/` | **Deps:** `golang.org/x/net`, `golang.org/x/text`

Monitors product prices across four major Japanese e-commerce platforms. Features platform-specific HTML parsers, polite crawling with User-Agent rotation and randomized delays, automatic Shift-JIS/EUC-JP charset detection and decoding, an alert engine, and a CLI scrape mode with table/JSON/CSV output. Also includes a terminal-aesthetic management dashboard.

**Supported platforms**

| Platform | Domain | Parser Strategy |
|----------|--------|-----------------|
| 楽天市場 | item.rakuten.co.jp | CSS selector cascade → regex fallback |
| Yahoo!ショッピング | shopping.yahoo.co.jp | CSS selectors + PayPay bonus extraction |
| Amazon.co.jp | amazon.co.jp | ID-based selectors (`priceblock_ourprice`, etc.) |
| メルカリ | jp.mercari.com | `__NEXT_DATA__` JSON extraction → regex fallback |

**Endpoints**

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/products` | All tracked products |
| `POST` | `/api/v1/products` | Add product with listing URLs |
| `GET` | `/api/v1/products/:id/history` | Price history (`?platform=&days=`) |
| `GET` | `/api/v1/products/:id/compare` | Cross-platform price comparison |
| `POST` | `/api/v1/products/:id/scrape` | Manual scrape trigger |
| `GET` | `/api/v1/alerts` | Alert list (`?unread=true`) |
| `POST` | `/api/v1/alerts/:id/read` | Mark alert as read |
| `GET` | `/api/v1/stats` | Scrape statistics per platform |

**Alert types**

| Type | Trigger |
|------|---------|
| `target_hit` | Price drops at or below the user's target price |
| `drop_pct` | Price drops by the configured percentage threshold |
| `new_low` | Price sets a new all-time low for this product/platform |
| `back_in_stock` | Item transitions from unavailable → available |

**Environment variables**

| Variable | Default | Description |
|----------|---------|-------------|
| `CHECK_INTERVAL` | `30m` | Auto-scrape polling interval |
| `AUTO_SCRAPE` | `false` | Enable scheduler on startup |
| `PORT` | `8085` | Listen port |

```bash
cd price-tracker && go mod tidy && go run main.go

# CLI mode — scrape a single URL
go run main.go scrape -platform rakuten -format table https://item.rakuten.co.jp/...
go run main.go scrape -platform amazon_jp -format json https://www.amazon.co.jp/dp/...
go run main.go scrape -platform mercari -format csv https://jp.mercari.com/item/...

# Cross-platform comparison
curl "http://localhost:8085/api/v1/products/prod-001/compare"
# → {"best_price":{"platform":"rakuten","current_price":36800},"comparisons":[...]}

# Open dashboard
open dashboard.html
```

> **Note on crawling ethics:** The scraper enforces a minimum 2-second delay between requests plus randomized jitter. Respect each platform's `robots.txt` and Terms of Service. This tool is intended for personal, non-commercial price monitoring.

---

### 07 — 振仮名ミドルウェア (Furigana Injection Middleware)

**Port:** `8086` | **Path:** `./furigana/` | **Deps:** `golang.org/x/net`

Go HTTP middleware that automatically wraps kanji in HTML responses with `<ruby>` furigana annotations. Ships with a 3,000-word built-in dictionary using a greedy 8→1 character longest-match scan — zero CGO, zero external binaries, deploys as a single static binary. Production use cases can swap in MeCab or Kuromoji via the `Annotator` interface without modifying the middleware layer.

**Usage — as middleware**

```go
import "github.com/yourname/furigana"

mw := furigana.New(furigana.Options{
    Mode:  furigana.ModeHiragana, // ModeHiragana | ModeKatakana | ModeRomaji
    Level: furigana.LevelAll,     // LevelAll | LevelJLPTN5 | LevelJLPTN4 | LevelJLPTN3 | LevelAdvanced
    Debug: true,
})

http.Handle("/", mw.Wrap(myHandler))
// Response headers added automatically:
// X-Furigana-Injected: 12
// X-Furigana-Skipped:  2
// X-Furigana-Backend:  built-in-dict
```

**Usage — standalone function**

```go
mw := furigana.New(furigana.Options{})
result, stats, err := mw.InjectFurigana(`<p>東京は日本の首都です。</p>`)
// result: <p><ruby><rb>東京</rb><rt>とうきょう</rt></ruby>は...
// stats.Injected = 3, stats.Skipped = 0
```

**Annotator interface — plug in any backend**

```go
type Annotator interface {
    Annotate(text string) ([]Token, error)
    Name() string
}

// MeCab via subprocess IPC (no CGO required)
type MeCabAnnotator struct{ dicdir string }

func (m *MeCabAnnotator) Annotate(text string) ([]Token, error) {
    cmd := exec.Command("mecab", "-d", m.dicdir, "-O", "chasen")
    cmd.Stdin = strings.NewReader(text)
    out, err := cmd.Output()
    if err != nil { return nil, err }
    return ParseMeCabOutput(string(out)), nil
}

mw := furigana.New(furigana.Options{
    Annotator: &MeCabAnnotator{dicdir: "/usr/lib/mecab/dic/mecab-ipadic-neologd"},
})
```

**Skip rules**

```html
<!-- Skip by CSS class -->
<p class="no-furigana">この漢字にはふりがなが付きません</p>

<!-- Skip by attribute -->
<div data-furigana="skip">スキップされます</div>

<!-- These tags are always skipped: script, style, pre, code, kbd, ruby, head, title -->
<pre><code>func main() { fmt.Println("日本語") }</code></pre>
```

**API endpoints**

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/annotate` | Annotate text or inject into HTML (JSON body) |
| `POST` | `/api/v1/annotate` | Inject into raw HTML (text/html body) |
| `GET` | `/api/v1/lookup?word=東京` | Look up reading for a word |
| `GET` | `/api/v1/stats` | Middleware configuration and dictionary stats |

```bash
cd furigana && go mod tidy && go run main.go serve

# Annotate text
curl -X POST http://localhost:8086/api/v1/annotate \
  -H 'Content-Type: application/json' \
  -d '{"text":"東京都は日本の首都です","mode":"hiragana"}'

# Look up a word
curl "http://localhost:8086/api/v1/lookup?word=東京"
# → {"word":"東京","reading":"とうきょう","katakana":"トウキョウ","romaji":"toukyou","jlpt_level":5}

# Inject from stdin
echo "<p>日本語の勉強</p>" | go run ./furigana annotate -mode romaji

# Open interactive demo
open demo.html
```

---

## Repository Structure

```
go-japan-portfolio/
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
│   ├── demo.html          # Standalone browser demo (no server)
│   ├── go.mod
│   ├── Dockerfile
│   └── README.md
│
├── kintai-api/            # 05 — 勤怠管理 API
│   ├── main.go
│   ├── dashboard.html     # 7-tab management dashboard
│   ├── go.mod
│   ├── Dockerfile
│   └── README.md
│
├── price-tracker/         # 06 — 価格追跡サービス
│   ├── main.go
│   ├── dashboard.html     # Terminal-style price dashboard
│   ├── go.mod
│   ├── go.sum
│   ├── Dockerfile
│   └── README.md
│
├── furigana/              # 07 — 振仮名ミドルウェア
│   ├── main.go
│   ├── demo.html          # Interactive annotation demo
│   ├── go.mod
│   ├── go.sum
│   ├── Dockerfile
│   └── README.md
│
├── docker-compose.yml     # Starts all 7 services
├── README.md              # This file
└── LICENSE
```

---

## Quick Start

### Run everything with Docker Compose

```bash
git clone https://github.com/yourname/go-japan-portfolio
cd go-japan-portfolio
docker-compose up -d
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

Each service is a self-contained module:

```bash
# Any service — same pattern
cd holidays-api
go run main.go

# Services with external deps
cd price-tracker
go mod tidy
go run main.go
```

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
| **Containers** | Multi-stage Docker builds; scratch or alpine base; 5–15 MB images |
| **CGO** | None — all services compile with `CGO_ENABLED=0` |
| **Docs** | Bilingual README (Japanese + English); Zenn/Qiita-ready markdown |

External dependencies are intentionally minimal. Five of the seven services use only the Go standard library. The two exceptions (`price-tracker` and `furigana`) add `golang.org/x/net` and `golang.org/x/text` from the official Go extended packages — these are the only non-stdlib imports in the entire portfolio.

---

## Design Principles

**Stdlib first.** The default answer to "should I add a package?" is no. Every external dependency is a deployment constraint, a security surface, and a maintenance burden. This portfolio demonstrates that a surprising amount of production-relevant functionality — HTTP routing, JSON serialization, concurrency, HTML parsing — is available in the Go standard library without any framework.

**Japanese law as code.** Three projects implement Japanese legal requirements as executable logic rather than configuration: the attendance API encodes 労働基準法 articles 32, 34, 37, and 39 as calculation functions; the invoice generator validates the 2023 インボイス制度 T-number format; the holiday API derives public holiday dates from the text of 国民の祝日に関する法律. The goal is for the code to read as a faithful translation of the law.

**Interface-driven extensibility.** The furigana middleware's `Annotator` interface lets any team substitute MeCab, Kuromoji, or a cloud NLP API without touching the HTTP layer. The price tracker's platform scrapers implement a common `ScrapeProduct` dispatcher pattern. New platforms and backends slot in without modifying the core.

**Encoding awareness.** Legacy Japanese systems frequently produce Shift-JIS and EUC-JP output. The price tracker detects the charset from the `Content-Type` response header and transparently decodes it before HTML parsing — a real-world problem that most scraping tutorials skip over.

**Concurrency safety.** Every in-memory store uses `sync.RWMutex` with explicit lock discipline: read operations hold `RLock`, write operations hold the full `Lock`. This isn't theoretical — the postal code API is designed to handle thousands of concurrent lookups on a single instance without data races.

**Bilingual documentation.** Every README, every API response field name, and every error message is designed to be readable by both Japanese and English-speaking developers. Zenn.dev and Qiita publishing guides accompany each project.

---

## License

MIT License — see [LICENSE](./LICENSE) for details.

---

*Built as a portfolio targeting the Japanese software engineering market.*  
*Published on [Zenn.dev](https://zenn.dev/yourname) · [Qiita](https://qiita.com/yourname) · [GitHub](https://github.com/yourname)*
