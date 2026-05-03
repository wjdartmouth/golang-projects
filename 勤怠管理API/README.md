# 🕐 勤怠管理API (Attendance Management API)

労働基準法に準拠した勤怠管理・残業計算・有給休暇管理のGoマイクロサービスです。打刻・月次サマリー・36協定アラートを提供します。

> A zero-dependency Go REST API for Japanese employee attendance management — clock-in/out, 労基法-compliant overtime calculation, 有給休暇 (paid leave) tracking, and monthly reporting with labor law alerts.

---

## 特徴 / Features

- ✅ **打刻管理** — 出勤・退勤・休憩開始・終了の打刻API
- ✅ **残業計算** — 労基法第32条準拠（8時間/40時間超を法定外残業として自動算出）
- ✅ **深夜残業** — 22:00〜05:00の深夜割増（1.35倍）を自動計算
- ✅ **有給休暇** — 労基法第39条準拠の付与日数計算・申請・承認ワークフロー
- ✅ **月次サマリー** — 出勤日数・残業時間・賃金概算・労務アラートを一括出力
- ✅ **36協定アラート** — 月45時間超・年360時間超の警告
- ✅ **インタラクティブダッシュボード** — `dashboard.html` でブラウザ上でデモ可能
- ✅ **ゼロ外部依存** — 標準ライブラリのみ

---

## クイックスタート

```bash
git clone https://github.com/yourname/kintai-api
cd kintai-api
go run main.go
# → http://localhost:8084

# ダッシュボードを開く
open dashboard.html
```

---

## 労働法準拠内容 / Labor Law Compliance

| 条文 | 内容 | 実装 |
|------|------|------|
| 労基法第32条 | 法定労働時間 8h/日、40h/週 | 超過分を自動計算 |
| 労基法第34条 | 休憩時間（6h超→45分、8h超→60分） | 自動付与 |
| 労基法第37条 | 残業割増率 1.25倍、深夜 1.35倍 | 給与計算に反映 |
| 労基法第39条 | 年次有給休暇の付与日数 | 勤続年数から自動算出 |
| 36協定目安 | 月45時間・年360時間 | アラート自動生成 |

---

## APIエンドポイント

### 従業員管理

| Method | Path | 説明 |
|--------|------|------|
| GET | `/api/v1/employees` | 従業員一覧（?department=…） |
| POST | `/api/v1/employees` | 従業員登録 |
| GET | `/api/v1/employees/:id` | 従業員詳細 |
| PUT | `/api/v1/employees/:id` | 従業員情報更新 |

### 打刻

| Method | Path | 説明 |
|--------|------|------|
| POST | `/api/v1/clock` | 出勤・退勤・休憩打刻 |

**リクエスト例:**
```json
{
  "employee_id": "emp-001",
  "type": "clock_in",
  "location": "office",
  "note": "在宅勤務"
}
```

`type` は `clock_in` / `clock_out` / `break_start` / `break_end`

### 勤怠

| Method | Path | 説明 |
|--------|------|------|
| GET | `/api/v1/attendance?employee_id=&month=` | 月次勤怠一覧 |
| GET | `/api/v1/attendance?employee_id=&date=` | 日次勤怠 |
| PUT | `/api/v1/attendance/:empID/:date` | 勤怠修正（申請対応） |

### 有給休暇

| Method | Path | 説明 |
|--------|------|------|
| GET | `/api/v1/leave?employee_id=` | 有給申請一覧 |
| POST | `/api/v1/leave` | 有給申請 |
| POST | `/api/v1/leave/:id/approve` | 承認・却下 |
| GET | `/api/v1/leave/balance?employee_id=` | 有給残高照会 |

**有給申請リクエスト:**
```json
{
  "employee_id": "emp-003",
  "type": "有給休暇",
  "start_date": "2025-06-02",
  "end_date": "2025-06-04",
  "days": 3,
  "reason": "夏季休暇"
}
```

**承認リクエスト:**
```json
{
  "action": "approve",
  "approved_by": "emp-002"
}
```

### レポート

| Method | Path | 説明 |
|--------|------|------|
| GET | `/api/v1/reports/monthly?employee_id=&month=` | 月次勤怠サマリー |
| GET | `/api/v1/reports/overtime?employee_id=&month=` | 残業明細 |
| GET | `/api/v1/stats` | 全社統計 |

---

## 月次サマリーレスポンス例

```json
{
  "employee_id": "emp-001",
  "employee_name": "田中 太郎",
  "year": 2025,
  "month": 5,
  "work_days": 18,
  "scheduled_days": 20,
  "absent_days": 0,
  "leave_days": 1,
  "total_work_minutes": 2340,
  "legal_work_minutes": 1920,
  "overtime_minutes": 420,
  "late_night_minutes": 90,
  "base_wage": 80000,
  "overtime_pay": 4375,
  "total_wage": 84375,
  "alerts": [
    "⚡ 月間残業7.0h"
  ]
}
```

---

## 有給付与日数（労基法第39条）

| 勤続年数 | 付与日数 |
|----------|---------|
| 6ヶ月 | 10日 |
| 1年6ヶ月 | 11日 |
| 2年6ヶ月 | 12日 |
| 3年6ヶ月 | 14日 |
| 4年6ヶ月 | 16日 |
| 5年6ヶ月 | 18日 |
| 6年6ヶ月以上 | 20日 |

---

## アーキテクチャ / Architecture

```
kintai-api/
├── main.go         # HTTPサーバー・ビジネスロジック・シードデータ
├── dashboard.html  # スタンドアロン管理ダッシュボード
├── go.mod          # モジュール（外部依存なし）
└── Dockerfile      # scratch ベース ~5MB
```

### データフロー

```
打刻 POST /clock
    ↓
ClockRecord 保存
    ↓
AttendanceRecord 更新（computeAttendance）
    ├── 実労働時間 = 総時間 - 休憩
    ├── 残業時間  = max(0, 実労働 - 480分)
    └── 深夜時間  = 22:00〜05:00 の重複分

月次レポート GET /reports/monthly
    ↓
buildMonthlySummary
    ├── 勤怠集計（日別ループ）
    ├── 賃金計算（基本 + 残業1.25倍 + 深夜0.35倍加算）
    └── アラート生成（36協定・有給未取得・欠勤）
```

### 同時実行安全性
```go
type Store struct {
    mu sync.RWMutex
    // ... すべてのMapをRWMutexで保護
}
// 読み取り: RLock / RUnlock
// 書き込み: Lock / Unlock
```

---

## 環境変数

| 変数 | デフォルト | 説明 |
|------|-----------|------|
| `PORT` | `8084` | リッスンポート（実装時に追加可） |
| `TZ` | システム | `Asia/Tokyo` 推奨 |

---

## Docker

```bash
docker build -t kintai-api .
docker run -p 8084:8084 -e TZ=Asia/Tokyo kintai-api
```

---

## ライセンス

MIT License © 2025

---

[Portfolio](https://yourname.dev) · [GitHub](https://github.com/yourname) · [Zenn](https://zenn.dev/yourname)
