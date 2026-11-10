#!/usr/bin/env bash
# Refresh slim OpenJLPT JSON under jlpt-quiz-api/data/ from upstream.
# Source: https://github.com/evanclan/OpenJLPT (CC BY-SA 4.0)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$ROOT/data"
BASE="https://raw.githubusercontent.com/evanclan/OpenJLPT/main/data/json"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading OpenJLPT JSON into $TMP ..."
for kind in vocab kanji grammar; do
  mkdir -p "$TMP/$kind"
  for n in 5 4 3 2 1; do
    curl -fsSL -o "$TMP/$kind/n$n.json" "$BASE/$kind/n$n.json"
    echo "  fetched $kind/n$n.json"
  done
done

python3 - "$TMP" "$DEST" << 'PY'
import json, pathlib, sys

src_root = pathlib.Path(sys.argv[1])
dst_root = pathlib.Path(sys.argv[2])

def best_example(examples):
    complete = []
    for ex in examples or []:
        if not isinstance(ex, dict):
            continue
        ja = (ex.get("ja") or "").strip()
        en = (ex.get("en") or "").strip()
        fu = (ex.get("furigana") or "").strip()
        if not ja or not en:
            continue
        item = {"ja": ja, "en": en}
        if fu:
            item["furigana"] = fu
        complete.append(item)
    if not complete:
        return None
    complete.sort(key=lambda e: (0 if e.get("furigana") else 1, len(e["ja"])))
    return complete[0]

def slim_vocab(items):
    out = []
    for it in items:
        rec = {
            "id": it.get("id"),
            "word": it.get("word"),
            "reading": it.get("reading") or "",
            "romaji": it.get("romaji") or "",
            "meanings": [m for m in (it.get("meanings") or []) if str(m).strip()],
            "level": it.get("level"),
            "pos": [p for p in (it.get("pos") or []) if str(p).strip()],
        }
        ex = best_example(it.get("examples") or [])
        if ex:
            rec["example"] = ex
        out.append(rec)
    return out

def slim_kanji(items):
    out = []
    for it in items:
        words = [w for w in (it.get("words") or []) if str(w).strip()][:8]
        rec = {
            "character": it.get("character"),
            "level": it.get("level"),
            "strokes": it.get("strokes"),
            "onyomi": [x for x in (it.get("onyomi") or []) if str(x).strip()],
            "kunyomi": [x for x in (it.get("kunyomi") or []) if str(x).strip()],
            "meanings": [m for m in (it.get("meanings") or []) if str(m).strip()],
        }
        if words:
            rec["words"] = words
        out.append(rec)
    return out

def slim_grammar(items):
    out = []
    for it in items:
        examples = []
        for ex in (it.get("examples") or []):
            if not isinstance(ex, dict):
                continue
            ja = (ex.get("ja") or "").strip()
            if not ja:
                continue
            item = {"ja": ja}
            fu = (ex.get("furigana") or "").strip()
            en = (ex.get("en") or "").strip()
            if fu:
                item["furigana"] = fu
            if en:
                item["en"] = en
            examples.append(item)
            if len(examples) >= 3:
                break
        rec = {
            "id": it.get("id"),
            "pattern": it.get("pattern"),
            "romaji": it.get("romaji") or "",
            "level": it.get("level"),
            "meaning": it.get("meaning") or "",
            "formation": it.get("formation") or "",
        }
        notes = (it.get("notes") or "").strip()
        if notes:
            rec["notes"] = notes
        if examples:
            rec["examples"] = examples
        out.append(rec)
    return out

slimers = {"vocab": slim_vocab, "kanji": slim_kanji, "grammar": slim_grammar}
for kind in ("vocab", "kanji", "grammar"):
    for n in (5, 4, 3, 2, 1):
        raw = json.loads((src_root / kind / f"n{n}.json").read_text())
        slim = slimers[kind](raw)
        dest = dst_root / kind / f"n{n}.json"
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(json.dumps(slim, ensure_ascii=False, separators=(",", ":")) + "\n")
        print(f"wrote {dest.relative_to(dst_root.parent)}  ({len(slim)} items, {dest.stat().st_size:,} bytes)")
PY

echo "Done. Attribution: see $DEST/NOTICE.md (CC BY-SA 4.0)."
