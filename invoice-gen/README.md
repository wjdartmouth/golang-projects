# 🧾 請求書 API (Invoice Generator)

インボイス制度（適格請求書等保存方式）に対応した請求書を生成するGoサービスです。標準税率10%と軽減税率8%を行ごとに計算し、税率別の内訳付きで JSON または印刷用 HTML を返します。

> A stdlib-only Go HTTP service that generates Japan-compliant invoices with 10% / 8% consumption tax breakdown and HTML or JSON output.

---

## 特徴 / Features

- ✅ **インボイス制度対応** — 登録番号の表示と税率別合計
- ✅ **標準10% / 軽減8%** — 行ごとの税率と税抜・税額・税込
- ✅ **HTML出力** — Noto Sans JP と `@media print` でブラウザから印刷可能
- ✅ **ゼロ外部依存** — 標準ライブラリのみ

ポートは `8081` です（コード上は固定）。

---

## クイックスタート / Quick Start

```bash
git clone https://github.com/wjdartmouth/golang-projects.git
cd golang-projects/invoice-gen
go run .
# → http://localhost:8081

# サンプル請求書（HTML）
open http://localhost:8081/api/v1/invoice/preview
```

モジュールパス: `github.com/wjdartmouth/golang-projects/invoice-gen`

---

## APIエンドポイント

| メソッド | パス | 説明 |
|--------|------|------|
| GET | `/api/v1/invoice/preview` | サンプル請求書を HTML で表示 |
| GET | `/api/v1/invoice/generate` | 同じサンプルを JSON で返す |
| POST | `/api/v1/invoice/generate` | リクエスト body から請求書を JSON 生成 |
| POST | `/api/v1/invoice/generate?format=html` | 同じ内容を HTML で返す |
| GET | `/health` | ヘルスチェック |

必須フィールドは `seller_name` と `buyer_name` です。`invoice_no` / `issue_date` / `due_date` を省略するとサーバー側で補完します。

```bash
curl -X POST http://localhost:8081/api/v1/invoice/generate \
  -H 'Content-Type: application/json' \
  -d '{
    "seller_name": "株式会社サンプルテック",
    "seller_address": "〒150-0001 東京都渋谷区神宮前1-1-1",
    "seller_registration_no": "T1234567890123",
    "buyer_name": "株式会社クライアント商事",
    "buyer_address": "〒100-0001 東京都千代田区丸の内1-1-1",
    "invoice_no": "INV-202601-0042",
    "issue_date": "2026-01-31",
    "due_date": "2026-02-28",
    "items": [
      {"description": "Webシステム開発", "quantity": 1, "unit": "式", "unit_price": 500000, "tax_rate": 0.10},
      {"description": "打ち合わせ用お茶・お菓子", "quantity": 3, "unit": "個", "unit_price": 1500, "tax_rate": 0.08, "is_reduced": true}
    ]
  }'
```

レスポンスには `tax_breakdown`（税率別小計）、`subtotal_ex_tax`、`total_tax`、`total_amount`、任意の `bank_info` と埋め込み `html` が含まれます。

---

## Docker

```bash
docker build -t invoice-gen .
docker run -p 8081:8081 invoice-gen
```

---

## ライセンス / License

MIT License © 2026 — see [LICENSE](../LICENSE)
