# 📚 JLPT語彙クイズAPI

JLPT N5〜N1の語彙データを提供するGoマイクロサービスです。フラッシュカードアプリ・クイズアプリのバックエンドとして使用できます。

> A zero-dependency Go backend for JLPT vocabulary flashcard and quiz applications. Covers N5–N1 with 4 quiz modes, session tracking, streak scoring, and spaced repetition support.

---

## 特徴 / Features

- ✅ **N5〜N1全レベル対応** — 漢字・かな・ローマ字・意味・用例
- ✅ **4つのクイズモード** — 漢字→意味 / 意味→漢字 / かな→漢字 / フラッシュカード
- ✅ **セッション管理** — インメモリのsync.RWMutex保護セッションストア
- ✅ **スコアリング** — 連続正解ボーナス（3連続×2倍、5連続×3倍）
- ✅ **復習リスト** — 間違えた単語を自動収集
- ✅ **インタラクティブデモ** — `demo.html` でブラウザ上で即プレイ可能
- ✅ **ゼロ外部依存** — 標準ライブラリのみ

---

## クイックスタート

```bash
git clone https://github.com/wjdartmouth/jlpt-quiz-api
cd jlpt-quiz-api
go run main.go
# → http://localhost:8083

# デモをブラウザで開く
open demo.html
```

---

## APIエンドポイント

### 語彙

| メソッド | パス | 説明 |
|--------|-----|------|
| GET | `/api/v1/vocabulary` | 全単語一覧 (?level=N5&category=動詞) |
| GET | `/api/v1/vocabulary/random` | ランダム取得 (?level=N4&count=10) |
| GET | `/api/v1/vocabulary/:id` | 単語詳細 |

### クイズ

| メソッド | パス | 説明 |
|--------|-----|------|
| POST | `/api/v1/quiz/start` | クイズ開始・セッション生成 |
| POST | `/api/v1/quiz/answer` | 回答送信 |
| GET | `/api/v1/quiz/:id` | 現在の問題取得 |
| GET | `/api/v1/quiz/:id/summary` | 結果サマリー取得 |

### メタ

| メソッド | パス | 説明 |
|--------|-----|------|
| GET | `/api/v1/levels` | レベル別単語数・カテゴリ |
| GET | `/api/v1/stats` | 統計情報 |

---

## クイズの使い方 / Quiz Flow

### Step 1: クイズ開始
```bash
curl -X POST http://localhost:8083/api/v1/quiz/start \
  -H "Content-Type: application/json" \
  -d '{
    "level": "N4",
    "mode": "kanji_to_meaning",
    "count": 10
  }'
```

**レスポンス:**
```json
{
  "session_id": "sess-1706789012345678900",
  "level": "N4",
  "mode": "kanji_to_meaning",
  "total": 10,
  "first_question": {
    "session_id": "sess-...",
    "question_num": 1,
    "total": 10,
    "word_id": "n4-003",
    "prompt": "準備",
    "prompt_sub": "じゅんび",
    "choices": [
      { "id": "n4-003", "text": "preparation", "sub": "用意・支度", "correct": true },
      { "id": "n4-017", "text": "busy", "sub": "多忙な", "correct": false },
      { "id": "n4-001", "text": "experience", "sub": "体験・実経験", "correct": false },
      { "id": "n4-010", "text": "departure", "sub": "出発・旅立ち", "correct": false }
    ]
  }
}
```

### Step 2: 回答送信
```bash
curl -X POST http://localhost:8083/api/v1/quiz/answer \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "sess-1706789012345678900",
    "word_id": "n4-003",
    "choice_id": "n4-003"
  }'
```

**レスポンス:**
```json
{
  "correct": true,
  "correct_answer": "preparation",
  "score": 10,
  "correct_count": 1,
  "wrong_count": 0,
  "streak": 1,
  "session_finished": false,
  "next_question": { ... }
}
```

### Step 3: 結果取得
```bash
curl http://localhost:8083/api/v1/quiz/sess-xxx/summary
```

```json
{
  "total": 10,
  "correct": 8,
  "wrong": 2,
  "score": 100,
  "accuracy_pct": 80.0,
  "best_streak": 5,
  "grade": "A（良い）",
  "duration": "2m30s",
  "wrong_words": [...]
}
```

---

## クイズモード / Quiz Modes

| モード | 説明 | プロンプト | 選択肢 |
|--------|------|----------|------|
| `kanji_to_meaning` | 漢字→意味 | 漢字・かな | 意味 |
| `meaning_to_kanji` | 意味→漢字 | 英語意味 | 漢字 |
| `kana_to_kanji` | かな→漢字 | かな・ローマ字 | 漢字 |
| `flashcard` | フラッシュカード | 漢字・かな | 自己採点 |

---

## スコアリング / Scoring

| 状況 | 点数 |
|------|------|
| 正解 | +10 |
| 3連続正解 | +20（×2ボーナス） |
| 5連続正解 | +30（×3ボーナス） |
| 不正解 | ±0（連続リセット） |

---

## 成績判定 / Grading

| 正解率 | グレード |
|--------|---------|
| 95%以上 | S（優秀） |
| 80%以上 | A（良い） |
| 65%以上 | B（普通） |
| 50%以上 | C（要復習） |
| 50%未満 | D（要勉強） |

---

## アーキテクチャ / Architecture

```
jlpt-quiz-api/
├── main.go       # HTTPサーバー・クイズエンジン・語彙DB
├── demo.html     # スタンドアロン インタラクティブデモ
├── go.mod        # モジュール（外部依存なし）
└── Dockerfile    # scratch ベース ~5MB
```

### データ構造

```go
// セッションストア — goroutine-safe
type SessionStore struct {
    mu       sync.RWMutex
    sessions map[string]*QuizSession
}

// クイズセッション
type QuizSession struct {
    Words       []Word    // シャッフル済み問題プール
    CurrentIdx  int       // 現在の問題インデックス
    StreakCount int       // 連続正解数
    WrongWords  []Word    // 復習リスト
}
```

### スコアリングロジック

```go
func scoreForAnswer(isCorrect bool, streak int) int {
    if !isCorrect { return 0 }
    base := 10
    if streak >= 5 { return base * 3 }  // 🔥 ×3
    if streak >= 3 { return base * 2 }  // 🔥 ×2
    return base
}
```

---

## 語彙データ / Vocabulary Data

| レベル | 単語数 | カテゴリ数 |
|--------|--------|----------|
| N5 | 34語 | 6 |
| N4 | 20語 | 6 |
| N3 | 20語 | 5 |
| N2 | 15語 | 5 |
| N1 | 15語 | 4 |
| **合計** | **104語** | **14** |

カテゴリ: 時間・日付 / 数・量 / 家族・人 / 体・健康 / 食べ物・飲み物 / 交通・移動 / 仕事・学校 / 自然・天気 / 家・生活 / 感情・状態 / 動詞 / 形容詞 / 社会・文化 / 抽象・概念

---

## ライセンス / License

MIT License
