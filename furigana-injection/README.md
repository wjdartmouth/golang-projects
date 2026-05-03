# 振仮名注入ミドルウェア (Furigana Injection Middleware)

Go HTTP ミドルウェアがHTMLレスポンス中の漢字に自動的に `<ruby>` ふりがなを付与します。組み込み辞書（約3,000語）を使用し、CGO不要・外部バイナリなしで動作します。MeCabやKuromojiへの差し替えも `Annotator` インターフェース経由で簡単です。

> A Go HTTP middleware that automatically wraps kanji in HTML responses with `<ruby>` furigana annotations. Ships with a 3,000+ word built-in dictionary (zero CGO, zero external binaries). Swap in MeCab or Kuromoji via the `Annotator` interface without touching the middleware layer.

---

## 特徴 / Features

- ✅ **自動ふりがな注入** — HTMLレスポンス中の漢字を `<ruby><rb>漢字</rb><rt>かんじ</rt></ruby>` に変換
- ✅ **3種類の読み出力** — ひらがな・カタカナ・ローマ字（Hepburn式）
- ✅ **JLPTレベルフィルター** — N5のみ、N5+N4、全漢字、上級のみ など
- ✅ **スキップルール** — `<code>`, `<pre>`, `<script>`, `<ruby>` など除外タグ、`class="no-furigana"`, `data-furigana="skip"` 属性
- ✅ **冪等性** — 既にrubyタグが付いている要素は二重注釈しない
- ✅ **gzip対応** — gzip圧縮レスポンスを自動展開して処理
- ✅ **プラガブルバックエンド** — `Annotator` インターフェースでMeCab/Kuromoji切り替え可能
- ✅ **REST API** — テキスト注釈・HTML注入・単語検索エンドポイント
- ✅ **CLIモード** — `furigana annotate < page.html > annotated.html`
- ✅ **CGO不要** — `golang.org/x/net` のみ依存

---

## クイックスタート / Quick Start

```bash
git clone https://github.com/yourname/furigana
cd furigana
go mod tidy
go run main.go serve          # → http://localhost:8086
```

ブラウザで `demo.html` を開くとインタラクティブデモが利用できます。

---

## 使い方 / Usage

### ミドルウェアとして / As Middleware

```go
package main

import (
    "net/http"
    "github.com/yourname/furigana"
)

func main() {
    mw := furigana.New(furigana.Options{
        Mode:  furigana.ModeHiragana, // or ModeKatakana, ModeRomaji
        Level: furigana.LevelAll,     // or LevelJLPTN5, LevelJLPTN4, LevelAdvanced
        Debug: true,
    })

    // Wrap any http.Handler
    http.Handle("/", mw.Wrap(myHandler))
    http.ListenAndServe(":8086", nil)
}
```

レスポンスヘッダーに以下が自動付与されます:
```
X-Furigana-Injected: 12
X-Furigana-Skipped:  2
X-Furigana-Backend:  built-in-dict
```

### スタンドアロン関数として / Standalone

```go
mw := furigana.New(furigana.Options{})

html := `<p>東京は日本の首都です。</p>`
result, stats, err := mw.InjectFurigana(html)
// result: <p><ruby><rb>東京</rb><rt>とうきょう</rt></ruby>は
//         <ruby><rb>日本</rb><rt>にほん</rt></ruby>の
//         <ruby><rb>首都</rb><rt>しゅと</rt></ruby>です。</p>
// stats.Injected = 3, stats.Skipped = 0
```

---

## オプション / Options

| フィールド | 型 | デフォルト | 説明 |
|-----------|---|----------|------|
| `Annotator` | `Annotator` | `DictAnnotator` | 形態素解析バックエンド |
| `Mode` | `Mode` | `ModeHiragana` | 読みの出力形式 |
| `Level` | `Level` | `LevelAll` | 注釈するJLPTレベル |
| `SkipTags` | `[]string` | 下記参照 | 処理しないHTMLタグ |
| `SkipClass` | `[]string` | `["no-furigana"]` | スキップするCSSクラス |
| `AddCSS` | `bool` | `true` | ruby用スタイルを`<head>`に注入 |
| `MaxBodyBytes` | `int64` | `2MB` | 処理する最大ボディサイズ |
| `Debug` | `bool` | `false` | リクエスト毎に統計ログ |

**デフォルトSkipTags**: `script, style, pre, code, kbd, samp, var, textarea, button, input, select, option, abbr, acronym, ruby, rt, rb, head, title`

---

## Annotator インターフェース / Annotator Interface

```go
type Annotator interface {
    Annotate(text string) ([]Token, error)
    Name() string
}

type Token struct {
    Surface      string
    Reading      string // hiragana
    IsKanji      bool
    PartOfSpeech string
}
```

### MeCab統合例

```go
// MeCab via subprocess IPC (CGO不要)
type MeCabAnnotator struct { dicdir string }

func (m *MeCabAnnotator) Annotate(text string) ([]Token, error) {
    cmd := exec.Command("mecab", "-d", m.dicdir, "-O", "chasen")
    cmd.Stdin = strings.NewReader(text)
    out, _ := cmd.Output()
    return ParseMeCabOutput(string(out)), nil
}

// MeCab出力をトークンに変換 (提供済み関数)
tokens := ParseMeCabOutput(mecabOutput)

mw := furigana.New(furigana.Options{
    Annotator: &MeCabAnnotator{dicdir: "/usr/lib/mecab/dic/ipadic"},
})
```

### Kuromoji（Java）統合例

```go
// HTTP bridge to Kuromoji REST server
type KuromojiAnnotator struct { endpoint string }

func (k *KuromojiAnnotator) Annotate(text string) ([]Token, error) {
    resp, _ := http.Post(k.endpoint+"/tokenize",
        "text/plain", strings.NewReader(text))
    // parse JSON response...
}
```

---

## REST API

### POST /api/v1/annotate

テキストをトークナイズ:
```bash
curl -X POST http://localhost:8086/api/v1/annotate \
  -H 'Content-Type: application/json' \
  -d '{"text":"東京都は日本の首都です","mode":"hiragana"}'
```

HTMLに注入:
```bash
curl -X POST http://localhost:8086/api/v1/annotate \
  -H 'Content-Type: application/json' \
  -d '{"html":"<p>東京は日本の首都です</p>"}'
```

生HTMLボディ:
```bash
curl -X POST http://localhost:8086/api/v1/annotate \
  -H 'Content-Type: text/html' \
  --data-binary @page.html
```

### GET /api/v1/lookup?word=東京

```json
{
  "word":       "東京",
  "found":      true,
  "reading":    "とうきょう",
  "katakana":   "トウキョウ",
  "romaji":     "toukyou",
  "jlpt_level": 5,
  "pos":        "名詞",
  "ruby_html":  "<ruby><rb>東京</rb><rt>とうきょう</rt></ruby>"
}
```

### GET /api/v1/stats

バックエンド・辞書サイズ・設定情報を返します。

---

## CLIモード / CLI Mode

```bash
# HTMLファイルを変換
furigana annotate -mode hiragana < input.html > output.html

# カタカナで出力
furigana annotate -mode katakana < input.html > output.html

# ローマ字で出力
furigana annotate -mode romaji < input.html > output.html

# 単語を検索
furigana lookup 東京
furigana lookup 日本語
```

---

## スキップ方法 / How to Skip

```html
<!-- CSSクラスでスキップ -->
<p class="no-furigana">この漢字にはふりがなが付きません</p>

<!-- data属性でスキップ -->
<div data-furigana="skip">スキップされます</div>

<!-- コードブロックは自動除外 -->
<pre><code>func main() { fmt.Println("日本語") }</code></pre>
```

---

## アーキテクチャ / Architecture

```
furigana/
├── main.go       # ミドルウェア・API・CLI・辞書・ローマ字変換テーブル
├── demo.html     # インタラクティブデモ（サーバー不要で動作）
├── go.mod        # 依存: golang.org/x/net
└── Dockerfile    # scratch ベース ~5MB
```

処理パイプライン:
1. `responseCapture` でレスポンスボディをバッファ
2. Content-Type が `text/html` か確認
3. gzip展開（必要な場合）
4. `golang.org/x/net/html` でDOMをパース
5. DFS walkで全テキストノードを巡回（skipタグ・クラスを除外）
6. `Annotator.Annotate()` で漢字を検出・読みを取得（greedy最長一致）
7. JLPTレベルフィルター・読みモード変換
8. テキストノードをrubyフラグメントに置換
9. CSSを`<head>`に注入（冪等）
10. アノテーション済みHTMLをレスポンス

---

## ライセンス / License

MIT License © 2025

---

[Portfolio](https://yourname.dev) · [GitHub](https://github.com/yourname) · [Zenn](https://zenn.dev/yourname)
