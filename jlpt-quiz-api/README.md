# 📚 JLPTクイズAPI

[OpenJLPT](https://github.com/evanclan/OpenJLPT) の N5〜N1 データ（単語・漢字・文法）を埋め込んだ Go クイズサービスです。ブラウザで `http://localhost:8083` を開けば、同じプロセスが API とデモ UI を提供します。

> A stdlib-only Go quiz for JLPT N5–N1 vocabulary, kanji, and grammar. Data is adapted from OpenJLPT and embedded in the binary (`go:embed`). Open the server URL to play; no separate `file://` demo is required.

---

## 特徴 / Features

- ✅ **OpenJLPT データ** — 単語 約 7,811 / 漢字 2,383 / 文法 526（N5〜N1）
- ✅ **3つの種類** — 単語（vocab）・漢字（kanji）・文法（grammar）
- ✅ **複数モード** — 下記参照。単語の既存4モードは互換のまま
- ✅ **セッション採点** — インメモリ、連続正解ボーナス（3連続×2、5連続×3）
- ✅ **デモ UI** — サーバーが `/` で `demo.html` を配信（Docker でも同じ URL）
- ✅ **ゼロ外部依存** — 標準ライブラリのみ（`go:embed`, `net/http`, `encoding/json`）

---

## クイックスタート / Quick Start

```bash
git clone https://github.com/wjdartmouth/golang-projects.git
cd golang-projects/jlpt-quiz-api
go run .
# ブラウザで http://localhost:8083 を開く
```

このサービスはポートフォリオ用モノレポ `golang-projects` 内の `jlpt-quiz-api/` にあります。モジュールパスは `github.com/wjdartmouth/golang-projects/jlpt-quiz-api` です。

### 環境変数

| 変数 | デフォルト | 説明 |
|------|-----------|------|
| `PORT` | `8083` | リッスンポート。`0.0.0.0` にバインドします |

```bash
PORT=9090 go run .
```

### Docker

```bash
docker build -t jlpt-quiz-api .
docker run --rm -p 8083:8083 jlpt-quiz-api
# http://localhost:8083
```

リポジトリルートからなら `docker compose up jlpt-quiz-api` でも起動できます。イメージはマルチステージ・`CGO_ENABLED=0` で、データはバイナリに埋め込みます。

---

## クイズの種類とモード / Kinds and modes

`POST /api/v1/quiz/start` に `kind` を渡せます。省略時は `vocab`（後方互換）。

| kind | mode | 説明 |
|------|------|------|
| `vocab` | `kanji_to_meaning` | 単語を見て意味を選ぶ（デフォルト） |
| `vocab` | `meaning_to_kanji` | 意味を見て単語を選ぶ |
| `vocab` | `kana_to_kanji` | 読みを見て単語を選ぶ |
| `vocab` | `flashcard` | 裏面を見て自己採点（`correct` を送信） |
| `kanji` | `character_to_meaning` | 漢字 → 意味（漢字のデフォルト） |
| `kanji` | `meaning_to_character` | 意味 → 漢字 |
| `kanji` | `character_to_reading` | 漢字 → 音読み・訓読み |
| `grammar` | `pattern_to_meaning` | 文型 → 意味（文法のデフォルト） |
| `grammar` | `meaning_to_pattern` | 意味 → 文型 |

文法の回答後レスポンスには `formation`、`notes`、例文（`example_ja` / `example_furigana` / `example_en`）が含まれます。選択肢の JSON に正解フラグは含まれません。採点はサーバー側で行います。

---

## API

### クイズ

| メソッド | パス | 説明 |
|--------|------|------|
| POST | `/api/v1/quiz/start` | クイズ開始 |
| POST | `/api/v1/quiz/answer` | 回答送信 |
| GET | `/api/v1/quiz/:id` | 現在の問題 |
| GET | `/api/v1/quiz/:id/summary` | 結果サマリー |

```bash
# 単語クイズ
curl -s -X POST http://localhost:8083/api/v1/quiz/start \
  -H "Content-Type: application/json" \
  -d '{"level":"N5","kind":"vocab","mode":"kanji_to_meaning","count":5}'

# 漢字クイズ
curl -s -X POST http://localhost:8083/api/v1/quiz/start \
  -H "Content-Type: application/json" \
  -d '{"level":"N4","kind":"kanji","mode":"character_to_reading","count":5}'

# 文法クイズ
curl -s -X POST http://localhost:8083/api/v1/quiz/start \
  -H "Content-Type: application/json" \
  -d '{"level":"N5","kind":"grammar","mode":"pattern_to_meaning","count":5}'
```

回答:

```bash
curl -s -X POST http://localhost:8083/api/v1/quiz/answer \
  -H "Content-Type: application/json" \
  -d '{"session_id":"sess-…","item_id":"…","choice_id":"c…"}'
```

フラッシュカードでは `choice_id` の代わりに `"correct": true` または `false` を送ります。

### データ・メタ

| メソッド | パス | 説明 |
|--------|------|------|
| GET | `/` | デモ UI（`demo.html`） |
| GET | `/health` | ヘルスチェック（件数つき） |
| GET | `/api/v1/levels` | レベル別の単語・漢字・文法件数 |
| GET | `/api/v1/stats` | 合計件数・モード一覧 |
| GET | `/api/v1/vocabulary` | 単語一覧（`?level=N5`、`?pos=` / `?category=`） |
| GET | `/api/v1/vocabulary/random` | ランダム単語（`?count=` 最大 50） |
| GET | `/api/v1/vocabulary/:id` | 単語詳細 |

---

## データソース / Data source

問題データは [OpenJLPT](https://github.com/evanclan/OpenJLPT)（**CC BY-SA 4.0**）をクイズ用に縮小したコピーです。

| レベル | 単語 | 漢字 | 文法 |
|--------|------|------|------|
| N5 | 674 | 84 | 81 |
| N4 | 630 | 169 | 98 |
| N3 | 1,659 | 387 | 101 |
| N2 | 1,778 | 398 | 123 |
| N1 | 3,070 | 1,345 | 123 |
| **合計** | **7,811** | **2,383** | **526** |

件数は OpenJLPT 上流と一致します。JLPT 団体公式の出題範囲ではなく、Jonathan Waller 氏のコミュニティリスト等に基づく非公式の目安です。

縮小後のデータは `data/{vocab,kanji,grammar}/n5.json` 〜 `n1.json` にあり、起動時に `go:embed` で読み込みます。ファイルが欠けている、または空・不正な JSON の場合はサーバーは起動に失敗します。上流から再生成するには:

```bash
./scripts/refresh-openjlpt.sh
```

詳細なクレジットは [`data/NOTICE.md`](./data/NOTICE.md) を見てください。アプリのフッターにも OpenJLPT へのリンクがあります。

> Contains data from [OpenJLPT](https://github.com/evanclan/OpenJLPT) (CC BY-SA 4.0), which uses JMdict and KANJIDIC2 (EDRDG), Jonathan Waller's JLPT lists, and Tatoeba.

**ライセンス:** アプリケーションコードは MIT（リポジトリルートの [LICENSE](../LICENSE)）。`data/` 配下の JSON と NOTICE は **CC BY-SA 4.0** のままです。派生データセットも同じライセンスで公開する必要があります。

---

## スコアリング / Scoring

| 状況 | 点数 |
|------|------|
| 正解 | +10 |
| 3連続正解 | +20（×2） |
| 5連続正解 | +30（×3） |
| 不正解 | 0（連続リセット） |

成績: S（95%） / A（80%） / B（65%） / C（50%） / D（50%未満）。セッションはプロセス内のメモリにだけ保存されます。

---

## アーキテクチャ / Architecture

```
jlpt-quiz-api/
├── main.go                 # HTTP サーバー・クイズエンジン
├── demo.html               # 単一ファイルのデモ UI（/ で配信）
├── data/
│   ├── NOTICE.md           # CC BY-SA 帰属
│   ├── vocab/n5.json … n1.json
│   ├── kanji/n5.json … n1.json
│   └── grammar/n5.json … n1.json
├── scripts/refresh-openjlpt.sh
├── go.mod
├── Dockerfile
└── README.md
```

このサービスはポートフォリオ用の自己完結したクイズです。高可用な本番プラットフォームではありません。
