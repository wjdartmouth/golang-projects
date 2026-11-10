# 📮 郵便番号 API (Postal Code Lookup)

日本郵便の KEN_ALL.CSV を読み込み、郵便番号から住所を検索するGoサービスです。CSV が無い場合は15件のサンプルデータで起動します。

> A stdlib-only Go HTTP service for Japan Post postal-code lookup. Loads KEN_ALL.CSV when available, otherwise starts with 15 built-in sample records.

---

## 特徴 / Features

- ✅ **郵便番号検索** — ハイフンあり/なし（`150-0001` と `1500001`）の両方に対応
- ✅ **キーワード検索** — 住所（漢字）および都道府県・市区町村のカナを部分一致
- ✅ **形式チェック** — 7桁かどうか、およびデータ上に存在するかを返す
- ✅ **同時実行安全** — `sync.RWMutex` でインメモリマップを保護
- ✅ **ゼロ外部依存** — 標準ライブラリのみ

ポートは `8082` です（コード上は固定）。

---

## クイックスタート / Quick Start

```bash
git clone https://github.com/wjdartmouth/golang-projects.git
cd golang-projects/postal-api

# ゼロコンフィグ（組み込みサンプル15件）
go run .

# 日本郵便の CSV を使う場合
# POSTAL_CSV=/path/to/KEN_ALL.CSV go run .
```

モジュールパス: `github.com/wjdartmouth/golang-projects/postal-api`

`POSTAL_CSV` が未設定のときは `./KEN_ALL.CSV` を探し、ファイルが無ければサンプルデータにフォールバックします。CSV は [日本郵便の郵便番号データ](https://www.post.japanpost.jp/zipcode/download.html) から入手できます。

---

## APIエンドポイント

| メソッド | パス | 説明 |
|--------|------|------|
| GET | `/api/v1/postal/lookup?code=1500001` | 郵便番号で住所検索（ハイフン可） |
| GET | `/api/v1/postal/search?q=渋谷` | 住所・カナのキーワード検索（最大20件） |
| GET | `/api/v1/postal/validate?code=150-0001` | 7桁形式か、およびデータに存在するかを返す |
| GET | `/api/v1/postal/stats` | 件数と読み込み時刻 |
| GET | `/health` | ヘルスチェック |

```bash
curl "http://localhost:8082/api/v1/postal/lookup?code=100-0001"
# → {"postal_code":"1000001","count":1,"addresses":[{...}]}

curl "http://localhost:8082/api/v1/postal/search?q=渋谷"

curl "http://localhost:8082/api/v1/postal/validate?code=150-0001"
# → postal_code, is_valid_format, exists
```

ルックアップ結果の各住所には `postal_code`, `postal_code_fmt`, `prefecture`, `city`, `town`, かな、`full_address` が含まれます。組み込みサンプルには千代田・渋谷・新宿などの主要エリアが含まれます。

---

## Docker

```bash
docker build -t postal-api .
docker run -p 8082:8082 postal-api
# CSV を渡す場合:
# docker run -p 8082:8082 -e POSTAL_CSV=/data/KEN_ALL.CSV -v $PWD/KEN_ALL.CSV:/data/KEN_ALL.CSV postal-api
```

---

## ライセンス / License

MIT License © 2026 — see [LICENSE](../LICENSE)
