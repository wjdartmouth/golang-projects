# 🎌 祝日 API (Japanese Holidays API)

国民の祝日に関する法律に基づき、日本の祝日カレンダーを計算するGoサービスです。静的データベースではなく、固定祝日・ハッピーマンデー・春分/秋分・振替休日をコードで導出します。

> A stdlib-only Go HTTP service that computes Japan's national holiday calendar from the Act on National Holidays — fixed dates, Happy Monday rules, equinox approximations, and 振替休日 (substitute holidays).

---

## 特徴 / Features

- ✅ **法定祝日を計算** — 16の国民の祝日（固定日 + ハッピーマンデー + 春分・秋分の日）
- ✅ **振替休日** — 日曜と重なった祝日の振替、連休日の繰り越し
- ✅ **営業日判定** — 土日・祝日を除いた次の営業日
- ✅ **ゼロ外部依存** — 標準ライブラリのみ

---

## クイックスタート / Quick Start

```bash
git clone https://github.com/wjdartmouth/golang-projects.git
cd golang-projects/holidays-api
go run .
# → http://localhost:8080
```

モジュールパス: `github.com/wjdartmouth/golang-projects/holidays-api`

---

## APIエンドポイント

| メソッド | パス | 説明 |
|--------|------|------|
| GET | `/api/v1/holidays?year=2026` | 指定年の祝日一覧（省略時は今年）。年は 1948〜2100 |
| GET | `/api/v1/check?date=2026-01-01` | 祝日・営業日チェック（省略時は今日、`YYYY-MM-DD`） |
| GET | `/api/v1/next-business-day?from=2026-04-29` | 次の営業日（土日・祝日をスキップ） |
| GET | `/health` | ヘルスチェック |

```bash
curl "http://localhost:8080/api/v1/holidays?year=2026"

curl "http://localhost:8080/api/v1/check?date=2026-01-01"
# → date, weekday, weekday_ja, is_holiday, is_business_day, および該当すれば holiday

curl "http://localhost:8080/api/v1/next-business-day?from=2026-04-29"
# → from, next_business_day, weekday, weekday_ja
```

祝日オブジェクトのフィールドは `date`, `name`, `name_en`, `type`（`national` / `substitute` など）, `description` です。

---

## 実装メモ / Implementation notes

- ハッピーマンデー: 成人の日（1月第2月曜）、海の日（7月第3月曜）、敬老の日（9月第3月曜）、スポーツの日（10月第2月曜）
- 春分の日・秋分の日は年ごとの近似式で算出します
- 祝日が日曜と重なる場合は振替休日を追加し、翌日も祝日ならさらに繰り越します

---

## Docker

```bash
docker build -t holidays-api .
docker run -p 8080:8080 holidays-api
```

リポジトリルートから全サービスを起動する場合は `docker compose up` を使います。

---

## ライセンス / License

MIT License © 2026 — see [LICENSE](../LICENSE)
