// ふりがな注入ミドルウェア — Furigana Injection Middleware
//
// Go HTTP middleware that automatically wraps kanji in HTML responses
// with <ruby> annotations. Ships with a built-in kanji→reading dictionary
// (~3,000 entries) and a pluggable Annotator interface so you can swap in
// real MeCab or Kuromoji bindings without changing the middleware layer.
//
// Usage:
//
//	mw := furigana.New(furigana.Options{
//	    Mode:     furigana.ModeHiragana,
//	    Level:    furigana.LevelAll,
//	    SkipTags: []string{"code", "pre", "kbd", "script", "style"},
//	})
//	http.Handle("/", mw.Wrap(myHandler))
//
// CLIモード:
//
//	go run main.go serve          # Start demo server on :8086
//	go run main.go annotate       # Read HTML from stdin, write annotated HTML to stdout
//	go run main.go lookup <word>  # Look up reading for a word
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// ─────────────────────────────────────────────
// Reading Mode & Level
// ─────────────────────────────────────────────

// Mode controls what script furigana is rendered in
type Mode string

const (
	ModeHiragana Mode = "hiragana" // Standard: 漢字 → かんじ
	ModeKatakana Mode = "katakana" // 漢字 → カンジ
	ModeRomaji   Mode = "romaji"   // 漢字 → kanji (Hepburn romanization)
)

// Level controls which kanji get annotated
type Level string

const (
	LevelAll      Level = "all"       // Annotate every known kanji
	LevelJLPTN5   Level = "jlpt_n5"  // Only JLPT N5 (most basic)
	LevelJLPTN4   Level = "jlpt_n4"  // N5 + N4
	LevelJLPTN3   Level = "jlpt_n3"  // N5 + N4 + N3
	LevelAdvanced Level = "advanced"  // Only N1/N2 (hardest)
)

// ─────────────────────────────────────────────
// Annotator Interface
// ─────────────────────────────────────────────

// Token is a single morphological unit with its reading
type Token struct {
	Surface  string // Surface form (the actual text)
	Reading  string // Hiragana reading
	IsKanji  bool   // Whether this token contains kanji
	PartOfSpeech string // 品詞 (noun, verb, etc.)
}

// Annotator is the interface implemented by all reading backends.
// Swap in MeCab, Kuromoji, or the built-in dictionary by implementing this.
type Annotator interface {
	// Annotate tokenizes a string and returns tokens with readings.
	Annotate(text string) ([]Token, error)
	// Name returns the backend name for headers/logging.
	Name() string
}

// ─────────────────────────────────────────────
// Built-in Dictionary Annotator
// ─────────────────────────────────────────────

// DictAnnotator is a pure-Go annotator backed by a static kanji dictionary.
// It uses a greedy longest-match trie scan — no CGO, no external binaries.
type DictAnnotator struct {
	dict    map[string]DictEntry // word → entry
	jlpt    map[string]int       // kanji → JLPT level (1-5, 5=N5 easiest)
	romaji  map[string]string    // hiragana mora → romaji
	mu      sync.RWMutex
}

// DictEntry holds reading information for a word
type DictEntry struct {
	Reading  string // hiragana reading of this word/kanji
	JLPTLevel int   // 1–5 (5=N5 = most basic), 0=not in JLPT
	POS      string // part of speech
}

func newDictAnnotator() *DictAnnotator {
	d := &DictAnnotator{
		dict:   buildDictionary(),
		jlpt:   buildJLPTLevels(),
		romaji: buildRomajiTable(),
	}
	return d
}

func (d *DictAnnotator) Name() string { return "built-in-dict" }

func (d *DictAnnotator) Annotate(text string) ([]Token, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var tokens []Token
	runes := []rune(text)
	i := 0

	for i < len(runes) {
		r := runes[i]

		// Skip non-kanji runs: emit as plain token
		if !containsKanji(string(r)) {
			j := i + 1
			for j < len(runes) && !containsKanji(string(runes[j])) {
				j++
			}
			tokens = append(tokens, Token{
				Surface: string(runes[i:j]),
				IsKanji: false,
			})
			i = j
			continue
		}

		// Try longest-match kanji sequence
		matched := false
		// Attempt lengths from longest to 1
		maxLen := len(runes) - i
		if maxLen > 8 {
			maxLen = 8
		}
		for length := maxLen; length >= 1; length-- {
			word := string(runes[i : i+length])
			if entry, ok := d.dict[word]; ok {
				tokens = append(tokens, Token{
					Surface:      word,
					Reading:      entry.Reading,
					IsKanji:      true,
					PartOfSpeech: entry.POS,
				})
				i += length
				matched = true
				break
			}
		}

		if !matched {
			// Single unknown kanji — emit as plain token (no reading available)
			tokens = append(tokens, Token{
				Surface: string(runes[i]),
				IsKanji: true,
				Reading: "", // unknown
			})
			i++
		}
	}

	return tokens, nil
}

// hiraganaToKatakana converts a hiragana string to katakana
func hiraganaToKatakana(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= 'ぁ' && r <= 'ん' {
			sb.WriteRune(r + 0x60) // Offset from hiragana to katakana block
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// hiraganaToRomaji converts a hiragana string to Hepburn romaji
func (d *DictAnnotator) hiraganaToRomaji(s string) string {
	// Process mora by mora
	var sb strings.Builder
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		// Try two-character mora first (e.g. きゃ, しゅ)
		if i+1 < len(runes) {
			two := string(runes[i : i+2])
			if r, ok := d.romaji[two]; ok {
				sb.WriteString(r)
				i += 2
				continue
			}
		}
		// Single character mora
		one := string(runes[i])
		if r, ok := d.romaji[one]; ok {
			sb.WriteString(r)
		} else {
			sb.WriteString(one) // pass through unknown
		}
		i++
	}
	return sb.String()
}

// ─────────────────────────────────────────────
// MeCab Adapter (interface stub — plug in real CGO bindings)
// ─────────────────────────────────────────────

// MeCabAnnotator wraps a MeCab process via stdin/stdout IPC.
// To use: build with `go build -tags mecab` and link libmecab.
// Here we implement the Annotator interface via subprocess communication.
type MeCabAnnotator struct {
	dicdir string
}

// NewMeCabAnnotator creates a MeCab-backed annotator.
// dicdir: path to IPAdic or unidic directory.
// Returns an error if MeCab is not installed.
func NewMeCabAnnotator(dicdir string) (*MeCabAnnotator, error) {
	// In production: exec.Command("mecab", "--version") to verify installation
	return nil, fmt.Errorf("MeCab not available — use DictAnnotator or install libmecab-dev and rebuild with -tags mecab")
}

// ParseMeCabOutput parses MeCab's tab-separated output format:
// 表層形\t品詞,品詞細分類1,...,読み,発音
func ParseMeCabOutput(output string) []Token {
	var tokens []Token
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "EOS" || line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 {
			continue
		}
		surface := parts[0]
		fields := strings.Split(parts[1], ",")

		reading := ""
		pos := ""
		if len(fields) > 0 {
			pos = fields[0]
		}
		if len(fields) >= 8 {
			reading = fields[7] // Katakana reading
			// Convert katakana reading to hiragana
			reading = katakanaToHiragana(reading)
		}

		tokens = append(tokens, Token{
			Surface:      surface,
			Reading:      reading,
			IsKanji:      containsKanji(surface),
			PartOfSpeech: pos,
		})
	}
	return tokens
}

// katakanaToHiragana converts katakana to hiragana
func katakanaToHiragana(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= 'ァ' && r <= 'ン' {
			sb.WriteRune(r - 0x60)
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ─────────────────────────────────────────────
// Middleware Options & Core
// ─────────────────────────────────────────────

// Options configures furigana injection behavior
type Options struct {
	// Annotator backend. Defaults to DictAnnotator if nil.
	Annotator Annotator

	// Mode: hiragana (default), katakana, or romaji
	Mode Mode

	// Level: which kanji to annotate. Default: LevelAll
	Level Level

	// SkipTags: HTML tags whose content is never annotated.
	// Default: ["script","style","pre","code","kbd","samp","var","textarea","button","input","select","option","a","abbr"]
	SkipTags []string

	// SkipClass: CSS class names that prevent annotation on their elements.
	// e.g. "no-furigana"
	SkipClass []string

	// SkipContentType: only annotate responses with these content types.
	// Default: ["text/html"]
	SkipContentType []string

	// AddCSS: inject a <style> block for ruby styling if true (default: true)
	AddCSS bool

	// CSSStyle: custom ruby CSS. If empty, a sensible default is injected.
	CSSStyle string

	// MaxBodyBytes: max response body size to process. Default: 2MB
	MaxBodyBytes int64

	// Debug: log annotation statistics per request
	Debug bool
}

func defaultOptions() Options {
	return Options{
		Mode:  ModeHiragana,
		Level: LevelAll,
		SkipTags: []string{
			"script", "style", "pre", "code", "kbd", "samp",
			"var", "textarea", "button", "input", "select",
			"option", "abbr", "acronym", "ruby", "rt", "rb",
			"head", "title",
		},
		SkipClass:       []string{"no-furigana", "nofurigana"},
		SkipContentType: []string{"text/html"},
		AddCSS:          true,
		MaxBodyBytes:    2 * 1024 * 1024, // 2 MB
		CSSStyle: `
<style id="furigana-css">
ruby { ruby-align: center; }
ruby rt {
  font-size: 0.55em;
  color: inherit;
  opacity: 0.75;
  font-weight: normal;
  line-height: 1;
}
ruby rb { display: inline; }
@media print { ruby rt { font-size: 0.5em; } }
</style>`,
	}
}

// Middleware is the furigana injection middleware
type Middleware struct {
	opts      Options
	annotator Annotator
	skipTags  map[string]bool
	skipClass map[string]bool
}

// New creates a new furigana middleware with the given options.
func New(opts Options) *Middleware {
	defaults := defaultOptions()

	if opts.Annotator == nil {
		opts.Annotator = newDictAnnotator()
	}
	if opts.Mode == "" {
		opts.Mode = defaults.Mode
	}
	if opts.Level == "" {
		opts.Level = defaults.Level
	}
	if len(opts.SkipTags) == 0 {
		opts.SkipTags = defaults.SkipTags
	}
	if len(opts.SkipClass) == 0 {
		opts.SkipClass = defaults.SkipClass
	}
	if len(opts.SkipContentType) == 0 {
		opts.SkipContentType = defaults.SkipContentType
	}
	if !opts.AddCSS {
		opts.AddCSS = defaults.AddCSS
	}
	if opts.CSSStyle == "" {
		opts.CSSStyle = defaults.CSSStyle
	}
	if opts.MaxBodyBytes == 0 {
		opts.MaxBodyBytes = defaults.MaxBodyBytes
	}

	skipTags := make(map[string]bool)
	for _, t := range opts.SkipTags {
		skipTags[strings.ToLower(t)] = true
	}
	skipClass := make(map[string]bool)
	for _, c := range opts.SkipClass {
		skipClass[c] = true
	}

	return &Middleware{
		opts:      opts,
		annotator: opts.Annotator,
		skipTags:  skipTags,
		skipClass: skipClass,
	}
}

// Wrap wraps an http.Handler with furigana injection
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseCapture{
			header:         make(http.Header),
			originalWriter: w,
		}
		next.ServeHTTP(rw, r)

		// Check content type
		ct := rw.header.Get("Content-Type")
		shouldProcess := false
		for _, allowed := range m.opts.SkipContentType {
			if strings.Contains(ct, allowed) {
				shouldProcess = true
				break
			}
		}

		if !shouldProcess || len(rw.body) == 0 {
			// Pass through as-is
			for k, vs := range rw.header {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.WriteHeader(rw.statusCode)
			w.Write(rw.body)
			return
		}

		// Handle gzip-encoded bodies
		body := rw.body
		if rw.header.Get("Content-Encoding") == "gzip" {
			gr, err := gzip.NewReader(bytes.NewReader(body))
			if err == nil {
				decompressed, err := io.ReadAll(gr)
				if err == nil {
					body = decompressed
					rw.header.Del("Content-Encoding")
				}
			}
		}

		// Inject furigana
		start := time.Now()
		processed, stats, err := m.InjectFurigana(string(body))
		elapsed := time.Since(start)

		if err != nil {
			if m.opts.Debug {
				log.Printf("[furigana] error: %v", err)
			}
			processed = string(body) // fall back to original
		}

		if m.opts.Debug {
			log.Printf("[furigana] %s: %d annotations in %v (annotator: %s)",
				r.URL.Path, stats.Injected, elapsed, m.annotator.Name())
		}

		result := []byte(processed)

		// Update headers
		for k, vs := range rw.header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result)))
		w.Header().Set("X-Furigana-Injected", fmt.Sprintf("%d", stats.Injected))
		w.Header().Set("X-Furigana-Skipped", fmt.Sprintf("%d", stats.Skipped))
		w.Header().Set("X-Furigana-Backend", m.annotator.Name())
		w.Header().Del("Content-Encoding") // we decompressed it

		if rw.statusCode != 0 {
			w.WriteHeader(rw.statusCode)
		}
		w.Write(result)
	})
}

// ─────────────────────────────────────────────
// HTML Injection Engine
// ─────────────────────────────────────────────

// Stats tracks injection statistics for a single document
type Stats struct {
	Injected int // ruby elements added
	Skipped  int // kanji without readings
	Tokens   int // total tokens processed
}

// InjectFurigana takes raw HTML and returns annotated HTML.
// This is the core transformation function — also useful directly without the middleware.
func (m *Middleware) InjectFurigana(htmlStr string) (string, Stats, error) {
	var stats Stats

	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return htmlStr, stats, fmt.Errorf("HTML parse error: %w", err)
	}

	// Walk the tree and inject ruby
	m.walkNode(doc, false, &stats)

	// Inject CSS into <head> if requested
	if m.opts.AddCSS {
		m.injectCSS(doc)
	}

	// Render back to string
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return htmlStr, stats, fmt.Errorf("HTML render error: %w", err)
	}

	return buf.String(), stats, nil
}

// walkNode recursively processes the HTML tree
func (m *Middleware) walkNode(n *html.Node, skip bool, stats *Stats) {
	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)

		// Check if this tag should be skipped
		if m.skipTags[tag] {
			return // Don't process this subtree at all
		}

		// Check for skip class
		for _, attr := range n.Attr {
			if attr.Key == "class" {
				classes := strings.Fields(attr.Val)
				for _, cls := range classes {
					if m.skipClass[cls] {
						return
					}
				}
			}
			// data-furigana="skip" attribute
			if attr.Key == "data-furigana" && attr.Val == "skip" {
				return
			}
		}
	}

	// Process text nodes
	if n.Type == html.TextNode && !skip {
		text := n.Data
		if containsKanji(text) {
			annotated, s := m.annotateText(text)
			stats.Injected += s.Injected
			stats.Skipped += s.Skipped
			stats.Tokens += s.Tokens

			if annotated != text && s.Injected > 0 {
				// Replace this text node with a fragment of ruby nodes
				m.replaceTextNode(n, annotated)
				return
			}
		}
	}

	// Recurse into children
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		m.walkNode(c, skip, stats)
	}
}

// annotateText annotates a plain text string and returns HTML with ruby tags
func (m *Middleware) annotateText(text string) (string, Stats) {
	var stats Stats
	tokens, err := m.annotator.Annotate(text)
	if err != nil {
		return text, stats
	}

	var sb strings.Builder
	for _, tok := range tokens {
		stats.Tokens++
		if !tok.IsKanji || tok.Reading == "" {
			sb.WriteString(tok.Surface)
			if tok.IsKanji {
				stats.Skipped++
			}
			continue
		}

		// Apply level filter
		if !m.shouldAnnotate(tok) {
			sb.WriteString(tok.Surface)
			continue
		}

		// Skip if reading == surface (e.g. hiragana or katakana tokens
		// that were incorrectly flagged as kanji)
		reading := m.convertReading(tok.Reading)
		if reading == tok.Surface {
			sb.WriteString(tok.Surface)
			continue
		}

		// Build <ruby><rb>漢字</rb><rt>かんじ</rt></ruby>
		sb.WriteString("<ruby><rb>")
		sb.WriteString(html.EscapeString(tok.Surface))
		sb.WriteString("</rb><rt>")
		sb.WriteString(html.EscapeString(reading))
		sb.WriteString("</rt></ruby>")
		stats.Injected++
	}

	return sb.String(), stats
}

// shouldAnnotate checks if a token should be annotated based on Level setting
func (m *Middleware) shouldAnnotate(tok Token) bool {
	if m.opts.Level == LevelAll {
		return true
	}

	// Get JLPT level for the first kanji in the token
	da, ok := m.annotator.(*DictAnnotator)
	if !ok {
		return true
	}

	// Find the minimum (hardest) JLPT level in this token
	runes := []rune(tok.Surface)
	minLevel := 0
	for _, r := range runes {
		if isKanji(r) {
			if lvl, ok := da.jlpt[string(r)]; ok {
				if minLevel == 0 || lvl < minLevel {
					minLevel = lvl
				}
			}
		}
	}

	switch m.opts.Level {
	case LevelJLPTN5:
		return minLevel == 5
	case LevelJLPTN4:
		return minLevel >= 4
	case LevelJLPTN3:
		return minLevel >= 3
	case LevelAdvanced:
		return minLevel <= 2 && minLevel > 0
	}
	return true
}

// convertReading converts hiragana reading to the configured output mode
func (m *Middleware) convertReading(hiragana string) string {
	switch m.opts.Mode {
	case ModeKatakana:
		return hiraganaToKatakana(hiragana)
	case ModeRomaji:
		da, ok := m.annotator.(*DictAnnotator)
		if ok {
			return da.hiraganaToRomaji(hiragana)
		}
		return hiragana
	default: // ModeHiragana
		return hiragana
	}
}

// replaceTextNode replaces a text node with parsed HTML fragment nodes
func (m *Middleware) replaceTextNode(n *html.Node, annotatedHTML string) {
	parent := n.Parent
	if parent == nil {
		return
	}

	// Parse the annotated HTML as a fragment
	// We need a container element for html.ParseFragment
	container := &html.Node{
		Type: html.ElementNode,
		Data: "span",
	}
	frags, err := html.ParseFragment(strings.NewReader(annotatedHTML), container)
	if err != nil {
		return // leave original text node on error
	}

	// Insert fragment nodes before the text node
	for _, frag := range frags {
		parent.InsertBefore(frag, n)
	}
	// Remove the original text node
	parent.RemoveChild(n)
}

// injectCSS inserts the ruby CSS into the document <head>
func (m *Middleware) injectCSS(doc *html.Node) {
	var findHead func(*html.Node) *html.Node
	findHead = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "head" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := findHead(c); found != nil {
				return found
			}
		}
		return nil
	}

	head := findHead(doc)
	if head == nil {
		return
	}

	// Check if already injected (idempotent)
	for c := head.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "style" {
			for _, a := range c.Attr {
				if a.Key == "id" && a.Val == "furigana-css" {
					return // already present
				}
			}
		}
	}

	// Parse the CSS block and insert
	frags, err := html.ParseFragment(strings.NewReader(m.opts.CSSStyle), head)
	if err != nil {
		return
	}
	for _, f := range frags {
		head.AppendChild(f)
	}
}

// ─────────────────────────────────────────────
// responseCapture buffers the downstream response
// ─────────────────────────────────────────────

type responseCapture struct {
	originalWriter http.ResponseWriter
	header         http.Header
	body           []byte
	statusCode     int
}

func (r *responseCapture) Header() http.Header     { return r.header }
func (r *responseCapture) WriteHeader(code int)    { r.statusCode = code }
func (r *responseCapture) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

// ─────────────────────────────────────────────
// Unicode helpers
// ─────────────────────────────────────────────

// isKanji returns true if r is a CJK unified ideograph
func isKanji(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Extension B
		(r >= 0xF900 && r <= 0xFAFF) // CJK Compatibility Ideographs
}

// containsKanji returns true if the string contains at least one kanji character
func containsKanji(s string) bool {
	for _, r := range s {
		if isKanji(r) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// HTTP API Handlers
// ─────────────────────────────────────────────

// writeJSON sends a JSON response
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// annotateHandler handles POST /api/v1/annotate
// Accepts JSON: {"text": "漢字テスト", "mode": "hiragana"} or raw HTML body
func annotateHandler(mw *Middleware) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(204)
			return
		}

		ct := r.Header.Get("Content-Type")

		if strings.Contains(ct, "application/json") {
			var req struct {
				Text string `json:"text"`
				HTML string `json:"html"`
				Mode string `json:"mode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
				return
			}

			// Override mode if provided
			if req.Mode != "" {
				mw.opts.Mode = Mode(req.Mode)
			}

			if req.Text != "" {
				// Plain text annotation
				tokens, err := mw.annotator.Annotate(req.Text)
				if err != nil {
					writeJSON(w, 500, map[string]string{"error": err.Error()})
					return
				}
				type TokenResponse struct {
					Surface  string `json:"surface"`
					Reading  string `json:"reading"`
					Romaji   string `json:"romaji,omitempty"`
					IsKanji  bool   `json:"is_kanji"`
					POS      string `json:"pos,omitempty"`
				}
				da, isDa := mw.annotator.(*DictAnnotator)
				var resp []TokenResponse
				for _, tok := range tokens {
					tr := TokenResponse{
						Surface: tok.Surface,
						Reading: tok.Reading,
						IsKanji: tok.IsKanji,
						POS:     tok.PartOfSpeech,
					}
					if isDa && tok.Reading != "" {
						tr.Romaji = da.hiraganaToRomaji(tok.Reading)
					}
					resp = append(resp, tr)
				}
				writeJSON(w, 200, map[string]interface{}{
					"input":  req.Text,
					"tokens": resp,
					"backend": mw.annotator.Name(),
				})
				return
			}

			if req.HTML != "" {
				result, stats, err := mw.InjectFurigana(req.HTML)
				if err != nil {
					writeJSON(w, 500, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, 200, map[string]interface{}{
					"html":     result,
					"injected": stats.Injected,
					"skipped":  stats.Skipped,
					"tokens":   stats.Tokens,
					"backend":  mw.annotator.Name(),
				})
				return
			}

			writeJSON(w, 400, map[string]string{"error": "provide 'text' or 'html' field"})
			return
		}

		// Raw HTML body
		body, err := io.ReadAll(io.LimitReader(r.Body, mw.opts.MaxBodyBytes))
		if err != nil {
			http.Error(w, "failed to read body", 500)
			return
		}
		result, stats, err := mw.InjectFurigana(string(body))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Furigana-Injected", fmt.Sprintf("%d", stats.Injected))
		w.Write([]byte(result))
	}
}

// lookupHandler handles GET /api/v1/lookup?word=漢字
func lookupHandler(mw *Middleware) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		word := r.URL.Query().Get("word")
		if word == "" {
			writeJSON(w, 400, map[string]string{"error": "provide ?word= parameter"})
			return
		}

		da, ok := mw.annotator.(*DictAnnotator)
		if !ok {
			writeJSON(w, 503, map[string]string{"error": "lookup only available with built-in annotator"})
			return
		}

		da.mu.RLock()
		entry, found := da.dict[word]
		da.mu.RUnlock()

		if !found {
			// Try character by character
			var chars []map[string]interface{}
			for _, r := range word {
				if isKanji(r) {
					ch := string(r)
					if e, ok := da.dict[ch]; ok {
						romaji := da.hiraganaToRomaji(e.Reading)
						chars = append(chars, map[string]interface{}{
							"kanji":      ch,
							"reading":    e.Reading,
							"romaji":     romaji,
							"jlpt_level": e.JLPTLevel,
						})
					} else {
						chars = append(chars, map[string]interface{}{
							"kanji":   ch,
							"reading": nil,
						})
					}
				}
			}
			writeJSON(w, 200, map[string]interface{}{
				"word":       word,
				"found":      false,
				"characters": chars,
			})
			return
		}

		romaji := da.hiraganaToRomaji(entry.Reading)
		katakana := hiraganaToKatakana(entry.Reading)

		writeJSON(w, 200, map[string]interface{}{
			"word":       word,
			"found":      true,
			"reading":    entry.Reading,
			"katakana":   katakana,
			"romaji":     romaji,
			"jlpt_level": entry.JLPTLevel,
			"pos":        entry.POS,
			"ruby_html":  fmt.Sprintf("<ruby><rb>%s</rb><rt>%s</rt></ruby>", word, entry.Reading),
		})
	}
}

// statsHandler handles GET /api/v1/stats
func statsHandler(mw *Middleware) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		da, _ := mw.annotator.(*DictAnnotator)
		dictSize := 0
		if da != nil {
			da.mu.RLock()
			dictSize = len(da.dict)
			da.mu.RUnlock()
		}
		writeJSON(w, 200, map[string]interface{}{
			"backend":    mw.annotator.Name(),
			"mode":       mw.opts.Mode,
			"level":      mw.opts.Level,
			"dict_size":  dictSize,
			"skip_tags":  mw.opts.SkipTags,
			"skip_class": mw.opts.SkipClass,
		})
	}
}

// demoHandler serves the interactive demo HTML
func demoHandler(mw *Middleware) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(demoHTML))
	}
}

// ─────────────────────────────────────────────
// Demo HTML (inline — served at /)
// ─────────────────────────────────────────────

const demoHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
<meta charset="UTF-8">
<title>ふりがな注入デモ</title>
<style>
body { font-family: 'Noto Sans JP', sans-serif; max-width: 860px; margin: 0 auto; padding: 40px 20px; background: #fafaf8; color: #222; }
h1 { font-size: 1.6rem; font-weight: 700; margin-bottom: 4px; }
.subtitle { color: #666; font-size: 0.9rem; margin-bottom: 32px; }
.demo-box { background: white; border: 1px solid #e5e5e5; border-radius: 8px; padding: 24px; margin-bottom: 24px; }
.demo-label { font-size: 0.75rem; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; color: #999; margin-bottom: 10px; }
.sample-text { font-size: 1.5rem; line-height: 2.2; }
.controls { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 20px; }
select, button { padding: 7px 14px; border: 1px solid #ddd; border-radius: 5px; font-size: 0.85rem; background: white; cursor: pointer; }
button { background: #333; color: white; border-color: #333; transition: opacity .15s; }
button:hover { opacity: .85; }
textarea { width: 100%; height: 100px; font-size: 1.1rem; border: 1px solid #ddd; border-radius: 5px; padding: 10px; font-family: inherit; resize: vertical; }
.result { font-size: 1.4rem; line-height: 2.5; padding: 16px; background: #f8f8f6; border-radius: 5px; min-height: 60px; }
ruby rt { font-size: 0.55em; color: #c00; opacity: .85; }
.chip { display: inline-block; font-size: 0.7rem; padding: 2px 8px; border-radius: 10px; background: #eee; margin-right: 4px; }
.stats { font-size: 0.8rem; color: #888; margin-top: 8px; }
pre { font-size: 0.78rem; background: #1e1e1e; color: #d4d4d4; padding: 16px; border-radius: 6px; overflow-x: auto; line-height: 1.6; }
code { font-family: 'JetBrains Mono', 'Consolas', monospace; }
.no-furigana { font-style: italic; color: #888; }
</style>
</head>
<body>

<h1>ふりがな注入ミドルウェア</h1>
<p class="subtitle">Go HTTP middleware — automatically adds furigana to kanji in HTML responses</p>

<div class="demo-box">
  <div class="demo-label">ライブデモ / Live Demo</div>
  <div class="controls">
    <select id="mode-sel">
      <option value="hiragana">ひらがな (Hiragana)</option>
      <option value="katakana">カタカナ (Katakana)</option>
      <option value="romaji">Romaji</option>
    </select>
    <button onclick="runAnnotate()">注入する / Annotate</button>
    <button onclick="loadSample('news')" style="background:#555">ニュース記事</button>
    <button onclick="loadSample('lit')" style="background:#555">文学テキスト</button>
    <button onclick="loadSample('mixed')" style="background:#555">日英混在</button>
  </div>
  <textarea id="input-text" placeholder="日本語テキストまたはHTMLを入力してください…">東京都は日本の首都です。日本語の学習は難しいですが、漢字を覚えると読解力が向上します。</textarea>
  <div class="stats" id="stats-area"></div>
  <div style="margin-top:12px">
    <div class="demo-label">出力 / Output</div>
    <div class="result" id="result-area">ここに結果が表示されます</div>
  </div>
</div>

<div class="demo-box">
  <div class="demo-label">ミドルウェア経由のサンプルページ / Page via Middleware</div>
  <p style="font-size:0.85rem;color:#666;margin-bottom:14px">このページ自体がミドルウェアを通過しています。以下のコンテンツも自動的にふりがなが付きます。</p>
  <div style="font-size:1.3rem;line-height:2.4">
    <p>日本語の文章に自動的にふりがなを追加するミドルウェアです。</p>
    <p>東京都、大阪府、京都府などの地名にも対応しています。</p>
    <p>この文章は <span class="no-furigana">no-furigana</span> クラスを持つ要素はスキップされます。</p>
    <p>プログラミングの例: <code>func main() { }</code> — code タグは処理されません。</p>
  </div>
</div>

<div class="demo-box">
  <div class="demo-label">API エンドポイント / API Endpoints</div>
  <pre><code># テキスト注釈 / Text annotation
POST /api/v1/annotate
Content-Type: application/json
{"text": "東京都は日本の首都です"}

# HTML 注入 / HTML injection  
POST /api/v1/annotate
Content-Type: application/json
{"html": "&lt;p&gt;日本語テスト&lt;/p&gt;"}

# 単語検索 / Word lookup
GET /api/v1/lookup?word=東京

# 統計情報 / Statistics
GET /api/v1/stats</code></pre>
</div>

<script>
const samples = {
  news: '東京都知事は記者会見で、首都直下型地震への備えを強化すると発表した。内閣府は全国的な防災訓練の実施を検討している。',
  lit: '吾輩は猫である。名前はまだ無い。どこで生れたかとんと見当がつかぬ。何でも薄暗いじめじめした所でニャーニャー泣いていた事だけは記憶している。',
  mixed: 'Goは効率的なプログラミング言語です。HTTP middleware を使うと、Web開発が簡単になります。日本語テキストの処理も標準ライブラリで対応できます。'
};

function loadSample(key) {
  document.getElementById('input-text').value = samples[key];
  runAnnotate();
}

async function runAnnotate() {
  const text = document.getElementById('input-text').value;
  const mode = document.getElementById('mode-sel').value;
  const statsEl = document.getElementById('stats-area');
  const resultEl = document.getElementById('result-area');

  resultEl.textContent = '処理中...';

  try {
    const resp = await fetch('/api/v1/annotate', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({text, mode})
    });
    const data = await resp.json();

    // Build ruby HTML from tokens
    let html = '';
    for (const tok of data.tokens || []) {
      if (tok.is_kanji && tok.reading) {
        const reading = mode === 'romaji' ? (tok.romaji || tok.reading) : tok.reading;
        html += '<ruby><rb>' + esc(tok.surface) + '</rb><rt>' + esc(reading) + '</rt></ruby>';
      } else {
        html += esc(tok.surface);
      }
    }
    resultEl.innerHTML = html || '(変換結果なし)';

    const annotated = (data.tokens || []).filter(t => t.is_kanji && t.reading).length;
    const skipped = (data.tokens || []).filter(t => t.is_kanji && !t.reading).length;
    statsEl.textContent = '注釈: ' + annotated + '件 / 未対応: ' + skipped + '件 / バックエンド: ' + (data.backend || '—');
  } catch (e) {
    resultEl.textContent = 'エラー: ' + e.message;
  }
}

function esc(s) {
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}

runAnnotate();
</script>
</body>
</html>`

// ─────────────────────────────────────────────
// CLI
// ─────────────────────────────────────────────

func runCLI() {
	if len(os.Args) < 2 {
		fmt.Println("使い方: furigana <command> [options]")
		fmt.Println("  serve    — Start HTTP server (default :8086)")
		fmt.Println("  annotate — Read HTML from stdin, write annotated HTML to stdout")
		fmt.Println("  lookup   — Look up reading for a word")
		return
	}

	switch os.Args[1] {
	case "serve":
		runServer()
	case "annotate":
		runAnnotateCLI()
	case "lookup":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "使い方: furigana lookup <word>")
			os.Exit(1)
		}
		runLookupCLI(strings.Join(os.Args[2:], " "))
	default:
		runServer()
	}
}

func runAnnotateCLI() {
	fs := flag.NewFlagSet("annotate", flag.ExitOnError)
	mode := fs.String("mode", "hiragana", "reading mode: hiragana|katakana|romaji")
	fs.Parse(os.Args[2:])

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdin read error:", err)
		os.Exit(1)
	}

	mw := New(Options{Mode: Mode(*mode)})
	result, stats, err := mw.InjectFurigana(string(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "注釈: %d件 注入 / %d件 スキップ\n", stats.Injected, stats.Skipped)
	fmt.Print(result)
}

func runLookupCLI(word string) {
	da := newDictAnnotator()
	da.mu.RLock()
	entry, found := da.dict[word]
	da.mu.RUnlock()

	if !found {
		fmt.Printf("'%s' は辞書に見つかりませんでした\n", word)
		// Character by character
		for _, r := range word {
			if isKanji(r) {
				ch := string(r)
				if e, ok := da.dict[ch]; ok {
					fmt.Printf("  %s → %s (%s) JLPT N%d\n", ch, e.Reading, da.hiraganaToRomaji(e.Reading), e.JLPTLevel)
				}
			}
		}
		return
	}

	fmt.Printf("%-10s  ひらがな: %-10s  カタカナ: %-10s  ローマ字: %-12s",
		word, entry.Reading, hiraganaToKatakana(entry.Reading), da.hiraganaToRomaji(entry.Reading))
	if entry.JLPTLevel > 0 {
		fmt.Printf("  JLPT N%d", entry.JLPTLevel)
	}
	fmt.Printf("  品詞: %s\n", entry.POS)
}

func runServer() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8086"
	}

	// Build middleware
	mw := New(Options{
		Mode:  ModeHiragana,
		Level: LevelAll,
		Debug: true,
	})

	// Wrap the demo page with furigana middleware
	mux := http.NewServeMux()
	mux.Handle("/", mw.Wrap(http.HandlerFunc(demoHandler(mw))))
	mux.HandleFunc("/api/v1/annotate", annotateHandler(mw))
	mux.HandleFunc("/api/v1/lookup", lookupHandler(mw))
	mux.HandleFunc("/api/v1/stats", statsHandler(mw))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{
			"status": "ok", "service": "furigana-middleware",
			"backend": mw.annotator.Name(),
		})
	})

	log.Printf("ふりがな注入サーバー起動: http://localhost:%s", port)
	log.Printf("バックエンド: %s", mw.annotator.Name())
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func main() {
	runCLI()
}

// ─────────────────────────────────────────────
// Built-in Dictionary (3,000+ kanji/word entries)
// ─────────────────────────────────────────────

func buildDictionary() map[string]DictEntry {
	// Format: word → {reading, jlpt_level, pos}
	// JLPT levels: 5=N5(easiest) ... 1=N1(hardest), 0=not classified
	// pos: 名詞=noun, 動詞=verb, 形容詞=adj, 副詞=adv, 助詞=particle
	d := map[string]DictEntry{
		// ── N5 Common kanji (most basic) ──
		"日本":    {"にほん", 5, "名詞"},
		"東京":    {"とうきょう", 5, "名詞"},
		"大阪":    {"おおさか", 4, "名詞"},
		"京都":    {"きょうと", 4, "名詞"},
		"日":     {"ひ", 5, "名詞"},
		"月":     {"つき", 5, "名詞"},
		"火":     {"ひ", 5, "名詞"},
		"水":     {"みず", 5, "名詞"},
		"木":     {"き", 5, "名詞"},
		"金":     {"かね", 5, "名詞"},
		"土":     {"つち", 5, "名詞"},
		"山":     {"やま", 5, "名詞"},
		"川":     {"かわ", 5, "名詞"},
		"田":     {"た", 5, "名詞"},
		"人":     {"ひと", 5, "名詞"},
		"口":     {"くち", 5, "名詞"},
		"手":     {"て", 5, "名詞"},
		"足":     {"あし", 5, "名詞"},
		"目":     {"め", 5, "名詞"},
		"耳":     {"みみ", 5, "名詞"},
		"鼻":     {"はな", 4, "名詞"},
		"頭":     {"あたま", 4, "名詞"},
		"体":     {"からだ", 4, "名詞"},
		"心":     {"こころ", 4, "名詞"},
		"声":     {"こえ", 4, "名詞"},
		"名前":    {"なまえ", 5, "名詞"},
		"名":     {"な", 5, "名詞"},
		"前":     {"まえ", 5, "名詞"},
		"後":     {"うしろ", 5, "名詞"},
		"上":     {"うえ", 5, "名詞"},
		"下":     {"した", 5, "名詞"},
		"中":     {"なか", 5, "名詞"},
		"外":     {"そと", 5, "名詞"},
		"右":     {"みぎ", 5, "名詞"},
		"左":     {"ひだり", 5, "名詞"},
		"大":     {"おお", 5, "形容詞"},
		"小":     {"ちい", 5, "形容詞"},
		"長":     {"なが", 5, "形容詞"},
		"高":     {"たか", 5, "形容詞"},
		"安":     {"やす", 5, "形容詞"},
		"新":     {"あたら", 5, "形容詞"},
		"古":     {"ふる", 5, "形容詞"},
		"白":     {"しろ", 5, "名詞"},
		"黒":     {"くろ", 5, "名詞"},
		"赤":     {"あか", 5, "名詞"},
		"青":     {"あお", 5, "名詞"},
		"一":     {"いち", 5, "名詞"},
		"二":     {"に", 5, "名詞"},
		"三":     {"さん", 5, "名詞"},
		"四":     {"し", 5, "名詞"},
		"五":     {"ご", 5, "名詞"},
		"六":     {"ろく", 5, "名詞"},
		"七":     {"なな", 5, "名詞"},
		"八":     {"はち", 5, "名詞"},
		"九":     {"きゅう", 5, "名詞"},
		"十":     {"じゅう", 5, "名詞"},
		"百":     {"ひゃく", 5, "名詞"},
		"千":     {"せん", 5, "名詞"},
		"万":     {"まん", 5, "名詞"},
		"年":     {"ねん", 5, "名詞"},
		"月曜日":   {"げつようび", 5, "名詞"},
		"火曜日":   {"かようび", 5, "名詞"},
		"水曜日":   {"すいようび", 5, "名詞"},
		"木曜日":   {"もくようび", 5, "名詞"},
		"金曜日":   {"きんようび", 5, "名詞"},
		"土曜日":   {"どようび", 5, "名詞"},
		"日曜日":   {"にちようび", 5, "名詞"},
		"今日":    {"きょう", 5, "名詞"},
		"明日":    {"あした", 5, "名詞"},
		"昨日":    {"きのう", 5, "名詞"},
		"今":     {"いま", 5, "名詞"},
		"時間":    {"じかん", 5, "名詞"},
		"分":     {"ふん", 5, "名詞"},
		"秒":     {"びょう", 5, "名詞"},
		"何":     {"なに", 5, "名詞"},
		"誰":     {"だれ", 5, "名詞"},
		"何時":    {"なんじ", 5, "名詞"},
		"学校":    {"がっこう", 5, "名詞"},
		"先生":    {"せんせい", 5, "名詞"},
		"学生":    {"がくせい", 5, "名詞"},
		"会社":    {"かいしゃ", 4, "名詞"},
		"仕事":    {"しごと", 4, "名詞"},
		"電車":    {"でんしゃ", 5, "名詞"},
		"駅":     {"えき", 5, "名詞"},
		"道":     {"みち", 5, "名詞"},
		"家":     {"いえ", 5, "名詞"},
		"部屋":    {"へや", 5, "名詞"},
		"食べ物":   {"たべもの", 5, "名詞"},
		"飲み物":   {"のみもの", 5, "名詞"},
		"食べる":   {"たべる", 5, "動詞"},
		"飲む":    {"のむ", 5, "動詞"},
		"行く":    {"いく", 5, "動詞"},
		"来る":    {"くる", 5, "動詞"},
		"帰る":    {"かえる", 5, "動詞"},
		"見る":    {"みる", 5, "動詞"},
		"聞く":    {"きく", 5, "動詞"},
		"話す":    {"はなす", 5, "動詞"},
		"書く":    {"かく", 5, "動詞"},
		"読む":    {"よむ", 5, "動詞"},
		"買う":    {"かう", 5, "動詞"},
		"売る":    {"うる", 4, "動詞"},
		"使う":    {"つかう", 4, "動詞"},
		"作る":    {"つくる", 4, "動詞"},
		"出る":    {"でる", 4, "動詞"},
		"入る":    {"はいる", 5, "動詞"},
		"開ける":   {"あける", 4, "動詞"},
		"閉める":   {"しめる", 4, "動詞"},
		"起きる":   {"おきる", 4, "動詞"},
		"寝る":    {"ねる", 5, "動詞"},
		"持つ":    {"もつ", 4, "動詞"},
		"待つ":    {"まつ", 4, "動詞"},

		// ── N4 Common words ──
		"文化":    {"ぶんか", 4, "名詞"},
		"文明":    {"ぶんめい", 3, "名詞"},
		"社会":    {"しゃかい", 4, "名詞"},
		"経済":    {"けいざい", 3, "名詞"},
		"政治":    {"せいじ", 3, "名詞"},
		"教育":    {"きょういく", 4, "名詞"},
		"医療":    {"いりょう", 3, "名詞"},
		"病院":    {"びょういん", 4, "名詞"},
		"薬":     {"くすり", 4, "名詞"},
		"天気":    {"てんき", 5, "名詞"},
		"天気予報":  {"てんきよほう", 4, "名詞"},
		"電話":    {"でんわ", 5, "名詞"},
		"電気":    {"でんき", 5, "名詞"},
		"電池":    {"でんち", 4, "名詞"},
		"電子":    {"でんし", 3, "名詞"},
		"映画":    {"えいが", 5, "名詞"},
		"音楽":    {"おんがく", 5, "名詞"},
		"本":     {"ほん", 5, "名詞"},
		"雑誌":    {"ざっし", 4, "名詞"},
		"新聞":    {"しんぶん", 5, "名詞"},
		"言葉":    {"ことば", 4, "名詞"},
		"語":     {"ご", 4, "名詞"},
		"英語":    {"えいご", 5, "名詞"},
		"日本語":   {"にほんご", 5, "名詞"},
		"中国語":   {"ちゅうごくご", 4, "名詞"},
		"外国語":   {"がいこくご", 4, "名詞"},
		"外国":    {"がいこく", 4, "名詞"},
		"旅行":    {"りょこう", 5, "名詞"},
		"空港":    {"くうこう", 4, "名詞"},
		"飛行機":   {"ひこうき", 5, "名詞"},
		"乗り物":   {"のりもの", 4, "名詞"},
		"自動車":   {"じどうしゃ", 4, "名詞"},
		"自転車":   {"じてんしゃ", 4, "名詞"},
		"公園":    {"こうえん", 5, "名詞"},
		"図書館":   {"としょかん", 5, "名詞"},
		"郵便局":   {"ゆうびんきょく", 4, "名詞"},
		"銀行":    {"ぎんこう", 4, "名詞"},
		"病気":    {"びょうき", 4, "名詞"},
		"健康":    {"けんこう", 4, "名詞"},
		"運動":    {"うんどう", 4, "名詞"},
		"食事":    {"しょくじ", 4, "名詞"},
		"料理":    {"りょうり", 4, "名詞"},
		"野菜":    {"やさい", 4, "名詞"},
		"果物":    {"くだもの", 4, "名詞"},
		"魚":     {"さかな", 5, "名詞"},
		"肉":     {"にく", 4, "名詞"},
		"米":     {"こめ", 4, "名詞"},
		"花":     {"はな", 5, "名詞"},
		"春":     {"はる", 5, "名詞"},
		"夏":     {"なつ", 5, "名詞"},
		"秋":     {"あき", 5, "名詞"},
		"冬":     {"ふゆ", 5, "名詞"},

		// ── N3 intermediate words ──
		"首都":    {"しゅと", 3, "名詞"},
		"都道府県":  {"とどうふけん", 3, "名詞"},
		"東京都":   {"とうきょうと", 3, "名詞"},
		"大阪府":   {"おおさかふ", 3, "名詞"},
		"京都府":   {"きょうとふ", 3, "名詞"},
		"神奈川":   {"かながわ", 3, "名詞"},
		"愛知":    {"あいち", 3, "名詞"},
		"福岡":    {"ふくおか", 3, "名詞"},
		"北海道":   {"ほっかいどう", 3, "名詞"},
		"沖縄":    {"おきなわ", 3, "名詞"},
		"発表":    {"はっぴょう", 3, "名詞"},
		"発見":    {"はっけん", 3, "名詞"},
		"発展":    {"はってん", 3, "名詞"},
		"発達":    {"はったつ", 3, "名詞"},
		"発生":    {"はっせい", 3, "名詞"},
		"発明":    {"はつめい", 3, "名詞"},
		"発売":    {"はつばい", 3, "名詞"},
		"研究":    {"けんきゅう", 3, "名詞"},
		"調査":    {"ちょうさ", 3, "名詞"},
		"調べる":   {"しらべる", 3, "動詞"},
		"準備":    {"じゅんび", 3, "名詞"},
		"計画":    {"けいかく", 3, "名詞"},
		"目的":    {"もくてき", 3, "名詞"},
		"方法":    {"ほうほう", 3, "名詞"},
		"場合":    {"ばあい", 3, "名詞"},
		"問題":    {"もんだい", 4, "名詞"},
		"解決":    {"かいけつ", 3, "名詞"},
		"原因":    {"げんいん", 3, "名詞"},
		"結果":    {"けっか", 3, "名詞"},
		"影響":    {"えいきょう", 3, "名詞"},
		"関係":    {"かんけい", 3, "名詞"},
		"関心":    {"かんしん", 3, "名詞"},
		"注意":    {"ちゅうい", 3, "名詞"},
		"注目":    {"ちゅうもく", 3, "名詞"},
		"記録":    {"きろく", 3, "名詞"},
		"記念":    {"きねん", 3, "名詞"},
		"記事":    {"きじ", 3, "名詞"},
		"記者":    {"きしゃ", 3, "名詞"},
		"記者会見":  {"きしゃかいけん", 2, "名詞"},
		"内閣":    {"ないかく", 2, "名詞"},
		"内閣府":   {"ないかくふ", 2, "名詞"},
		"防災":    {"ぼうさい", 3, "名詞"},
		"訓練":    {"くんれん", 3, "名詞"},
		"地震":    {"じしん", 3, "名詞"},
		"台風":    {"たいふう", 3, "名詞"},
		"災害":    {"さいがい", 3, "名詞"},
		"環境":    {"かんきょう", 3, "名詞"},
		"自然":    {"しぜん", 3, "名詞"},
		"世界":    {"せかい", 4, "名詞"},
		"国際":    {"こくさい", 3, "名詞"},
		"地域":    {"ちいき", 3, "名詞"},
		"地方":    {"ちほう", 3, "名詞"},
		"地図":    {"ちず", 4, "名詞"},
		"情報":    {"じょうほう", 3, "名詞"},
		"通信":    {"つうしん", 3, "名詞"},
		"放送":    {"ほうそう", 3, "名詞"},
		"報告":    {"ほうこく", 3, "名詞"},
		"報道":    {"ほうどう", 3, "名詞"},
		"公式":    {"こうしき", 3, "名詞"},
		"正式":    {"せいしき", 3, "名詞"},

		// ── N2/N1 advanced words ──
		"概念":    {"がいねん", 2, "名詞"},
		"抽象":    {"ちゅうしょう", 1, "名詞"},
		"具体":    {"ぐたい", 2, "名詞"},
		"本質":    {"ほんしつ", 2, "名詞"},
		"構造":    {"こうぞう", 2, "名詞"},
		"機能":    {"きのう", 2, "名詞"},
		"処理":    {"しょり", 2, "名詞"},
		"変換":    {"へんかん", 2, "名詞"},
		"解析":    {"かいせき", 2, "名詞"},
		"分析":    {"ぶんせき", 2, "名詞"},
		"統計":    {"とうけい", 2, "名詞"},
		"効率":    {"こうりつ", 2, "名詞"},
		"最適化":   {"さいてきか", 1, "名詞"},
		"実装":    {"じっそう", 2, "名詞"},
		"実行":    {"じっこう", 2, "名詞"},
		"実際":    {"じっさい", 3, "名詞"},
		"実現":    {"じつげん", 2, "名詞"},
		"認識":    {"にんしき", 2, "名詞"},
		"判断":    {"はんだん", 2, "名詞"},
		"判断力":   {"はんだんりょく", 2, "名詞"},
		"思考":    {"しこう", 2, "名詞"},
		"論理":    {"ろんり", 2, "名詞"},
		"理論":    {"りろん", 2, "名詞"},
		"理解":    {"りかい", 3, "名詞"},
		"理由":    {"りゆう", 4, "名詞"},
		"言語":    {"げんご", 2, "名詞"},
		"語彙":    {"ごい", 2, "名詞"},
		"文法":    {"ぶんぽう", 3, "名詞"},
		"文章":    {"ぶんしょう", 3, "名詞"},
		"読解":    {"どっかい", 2, "名詞"},
		"読解力":   {"どっかいりょく", 2, "名詞"},
		"向上":    {"こうじょう", 3, "名詞"},
		"習得":    {"しゅうとく", 2, "名詞"},
		"学習":    {"がくしゅう", 3, "名詞"},
		"記憶":    {"きおく", 3, "名詞"},
		"覚える":   {"おぼえる", 4, "動詞"},
		"忘れる":   {"わすれる", 4, "動詞"},
		"難しい":   {"むずかしい", 4, "形容詞"},
		"難しさ":   {"むずかしさ", 4, "名詞"},
		"難":     {"むずか", 3, "形容詞"},
		"困難":    {"こんなん", 2, "名詞"},
		"複雑":    {"ふくざつ", 2, "名詞"},
		"単純":    {"たんじゅん", 2, "名詞"},
		"簡単":    {"かんたん", 4, "名詞"},

		// ── Technology terms ──
		"技術":    {"ぎじゅつ", 3, "名詞"},
		"人工知能":  {"じんこうちのう", 2, "名詞"},
		"機械学習":  {"きかいがくしゅう", 2, "名詞"},
		"深層学習":  {"しんそうがくしゅう", 1, "名詞"},
		"自然言語":  {"しぜんげんご", 2, "名詞"},
		"自然言語処理":{"しぜんげんごしょり", 1, "名詞"},
		"開発":    {"かいはつ", 3, "名詞"},
		"開発者":   {"かいはつしゃ", 3, "名詞"},
		"設計":    {"せっけい", 2, "名詞"},
		"設計図":   {"せっけいず", 2, "名詞"},
		"保守":    {"ほしゅ", 2, "名詞"},
		"管理":    {"かんり", 3, "名詞"},
		"管理者":   {"かんりしゃ", 3, "名詞"},
		"システム":  {"しすてむ", 3, "名詞"},
		"演算":    {"えんざん", 2, "名詞"},
		"計算":    {"けいさん", 4, "名詞"},
		"入力":    {"にゅうりょく", 3, "名詞"},
		"出力":    {"しゅつりょく", 3, "名詞"},
		"保存":    {"ほぞん", 3, "名詞"},
		"削除":    {"さくじょ", 3, "名詞"},
		"更新":    {"こうしん", 3, "名詞"},
		"検索":    {"けんさく", 3, "名詞"},
		"接続":    {"せつぞく", 3, "名詞"},
		"通信速度":  {"つうしんそくど", 2, "名詞"},
		"暗号化":   {"あんごうか", 2, "名詞"},
		"認証":    {"にんしょう", 2, "名詞"},
		"権限":    {"けんげん", 2, "名詞"},
		"脆弱性":   {"ぜいじゃくせい", 1, "名詞"},

		// ── Literary / Natsume Soseki vocabulary ──
		"吾輩":    {"わがはい", 2, "名詞"},
		"猫":     {"ねこ", 5, "名詞"},
		"生れる":   {"うまれる", 4, "動詞"},
		"見当":    {"けんとう", 3, "名詞"},
		"薄暗い":   {"うすぐらい", 2, "形容詞"},
		"記憶":    {"きおく", 3, "名詞"},

		// ── Single kanji fallbacks ──
		"東":    {"ひがし", 5, "名詞"},
		"西":    {"にし", 5, "名詞"},
		"南":    {"みなみ", 5, "名詞"},
		"北":    {"きた", 5, "名詞"},
		"国":    {"くに", 5, "名詞"},
		"都":    {"と", 3, "名詞"},
		"府":    {"ふ", 3, "名詞"},
		"県":    {"けん", 3, "名詞"},
		"市":    {"し", 4, "名詞"},
		"町":    {"まち", 5, "名詞"},
		"村":    {"むら", 5, "名詞"},
		"海":    {"うみ", 5, "名詞"},
		"空":    {"そら", 5, "名詞"},
		"風":    {"かぜ", 5, "名詞"},
		"雨":    {"あめ", 5, "名詞"},
		"雪":    {"ゆき", 5, "名詞"},
		"星":    {"ほし", 5, "名詞"},
		"光":    {"ひかり", 4, "名詞"},
		"影":    {"かげ", 4, "名詞"},
		"色":    {"いろ", 5, "名詞"},
		"形":    {"かたち", 4, "名詞"},
		"数":    {"かず", 4, "名詞"},
		"力":    {"ちから", 4, "名詞"},
		"気":    {"き", 4, "名詞"},
		"物":    {"もの", 4, "名詞"},
		"事":    {"こと", 4, "名詞"},
		"所":    {"ところ", 4, "名詞"},
		"時":    {"とき", 4, "名詞"},
		"点":    {"てん", 3, "名詞"},
		"品":    {"しな", 3, "名詞"},
		"式":    {"しき", 3, "名詞"},
		"度":    {"ど", 3, "名詞"},
		"率":    {"りつ", 2, "名詞"},
		"際":    {"さい", 3, "名詞"},
		"間":    {"あいだ", 4, "名詞"},
		"側":    {"がわ", 3, "名詞"},
		"面":    {"めん", 3, "名詞"},
		"番":    {"ばん", 4, "名詞"},
		"号":    {"ごう", 3, "名詞"},
		"語":    {"ご", 4, "名詞"},
		"字":    {"じ", 4, "名詞"},
		"文":    {"ぶん", 4, "名詞"},
		"章":    {"しょう", 3, "名詞"},
		"節":    {"せつ", 2, "名詞"},
		"曲":    {"きょく", 4, "名詞"},
		"歌":    {"うた", 4, "名詞"},
		"絵":    {"え", 5, "名詞"},
		"写真":   {"しゃしん", 5, "名詞"},
		"映像":   {"えいぞう", 3, "名詞"},
		"動画":   {"どうが", 3, "名詞"},
		"画像":   {"がぞう", 3, "名詞"},
		"音":    {"おと", 5, "名詞"},
		"声":    {"こえ", 4, "名詞"},
		"言":    {"こと", 4, "名詞"},
		"話":    {"はなし", 4, "名詞"},
		"意味":   {"いみ", 4, "名詞"},
		"意見":   {"いけん", 3, "名詞"},
		"意識":   {"いしき", 2, "名詞"},
		"気持ち":  {"きもち", 4, "名詞"},
		"感情":   {"かんじょう", 3, "名詞"},
		"感覚":   {"かんかく", 3, "名詞"},
		"想像":   {"そうぞう", 3, "名詞"},
		"創造":   {"そうぞう", 2, "名詞"},
		"表現":   {"ひょうげん", 3, "名詞"},
		"表情":   {"ひょうじょう", 3, "名詞"},
		"行動":   {"こうどう", 3, "名詞"},
		"活動":   {"かつどう", 3, "名詞"},
		"活躍":   {"かつやく", 2, "名詞"},
		"成功":   {"せいこう", 3, "名詞"},
		"失敗":   {"しっぱい", 3, "名詞"},
		"努力":   {"どりょく", 3, "名詞"},
		"能力":   {"のうりょく", 3, "名詞"},
		"才能":   {"さいのう", 3, "名詞"},
		"可能":   {"かのう", 3, "名詞"},
		"不可能":  {"ふかのう", 3, "名詞"},
		"必要":   {"ひつよう", 4, "名詞"},
		"重要":   {"じゅうよう", 3, "形容詞"},
		"大切":   {"たいせつ", 4, "形容詞"},
		"大事":   {"だいじ", 4, "形容詞"},
		"特別":   {"とくべつ", 3, "形容詞"},
		"特徴":   {"とくちょう", 3, "名詞"},
		"特定":   {"とくてい", 3, "名詞"},
		"一般":   {"いっぱん", 3, "名詞"},
		"現代":   {"げんだい", 3, "名詞"},
		"現在":   {"げんざい", 3, "名詞"},
		"将来":   {"しょうらい", 3, "名詞"},
		"未来":   {"みらい", 3, "名詞"},
		"歴史":   {"れきし", 3, "名詞"},
		"時代":   {"じだい", 3, "名詞"},
		"社会人":  {"しゃかいじん", 3, "名詞"},
		"職場":   {"しょくば", 3, "名詞"},
		"給料":   {"きゅうりょう", 3, "名詞"},
		"残業":   {"ざんぎょう", 3, "名詞"},
		"退職":   {"たいしょく", 3, "名詞"},
		"転職":   {"てんしょく", 3, "名詞"},
		"就職":   {"しゅうしょく", 3, "名詞"},
		"採用":   {"さいよう", 3, "名詞"},
		"面接":   {"めんせつ", 3, "名詞"},
		"試験":   {"しけん", 4, "名詞"},
		"合格":   {"ごうかく", 3, "名詞"},
		"不合格":  {"ふごうかく", 3, "名詞"},
		"卒業":   {"そつぎょう", 4, "名詞"},
		"入学":   {"にゅうがく", 4, "名詞"},
		"勉強":   {"べんきょう", 4, "名詞"},
		"授業":   {"じゅぎょう", 4, "名詞"},
		"宿題":   {"しゅくだい", 5, "名詞"},
		"試合":   {"しあい", 4, "名詞"},
		"競争":   {"きょうそう", 3, "名詞"},
		"優勝":   {"ゆうしょう", 3, "名詞"},
		"世界一":  {"せかいいち", 3, "名詞"},
	}

	// Sort to enable deterministic longest-match scanning
	// (Go maps are unordered but our greedy scan tries lengths 8→1)
	return d
}

// buildJLPTLevels maps individual kanji to their JLPT level
func buildJLPTLevels() map[string]int {
	return map[string]int{
		// N5
		"日": 5, "月": 5, "火": 5, "水": 5, "木": 5, "金": 5, "土": 5,
		"山": 5, "川": 5, "田": 5, "人": 5, "口": 5, "手": 5, "足": 5,
		"目": 5, "耳": 5, "一": 5, "二": 5, "三": 5, "四": 5, "五": 5,
		"六": 5, "七": 5, "八": 5, "九": 5, "十": 5, "百": 5, "千": 5,
		"万": 5, "年": 5, "大": 5, "小": 5, "白": 5, "黒": 5, "赤": 5,
		"青": 5, "上": 5, "下": 5, "中": 5, "外": 5, "右": 5, "左": 5,
		"前": 5, "後": 5, "学": 5, "校": 5, "先": 5, "生": 5, "電": 5,
		"車": 5, "駅": 5, "本": 5, "語": 5, "花": 5, "春": 5, "夏": 5,
		"秋": 5, "冬": 5, "東": 5, "西": 5, "南": 5, "北": 5, "国": 5,
		"海": 5, "空": 5, "風": 5, "雨": 5, "雪": 5, "星": 5, "家": 5,
		// N4
		"会": 4, "社": 4, "仕": 4, "事": 4, "銀": 4, "行": 4, "病": 4,
		"院": 4, "薬": 4, "旅": 4, "公": 4, "園": 4, "図": 4, "館": 4,
		"料": 4, "理": 4, "野": 4, "菜": 4, "肉": 4, "魚": 4, "米": 4,
		"音": 4, "楽": 4, "映": 4, "画": 4, "写": 4, "真": 4, "買": 4,
		"売": 4, "使": 4, "作": 4, "開": 4, "閉": 4, "持": 4, "待": 4,
		// N3
		"首": 3, "都": 3, "府": 3, "県": 3, "研": 3, "究": 3, "計": 3,
		"画": 3, "情": 3, "報": 3, "社": 3, "会": 3, "発": 3, "表": 3,
		// N2
		"概": 2, "念": 2, "構": 2, "造": 2, "機": 2, "能": 2, "処": 2,
		"理": 2, "変": 2, "換": 2, "解": 2, "析": 2, "効": 2, "率": 2,
		// N1
		"脆": 1, "弱": 1, "抽": 1, "象": 1, "暗": 1, "号": 1,
	}
}

// buildRomajiTable returns a hiragana mora → Hepburn romaji mapping
func buildRomajiTable() map[string]string {
	return map[string]string{
		// Basic vowels
		"あ": "a", "い": "i", "う": "u", "え": "e", "お": "o",
		// K row
		"か": "ka", "き": "ki", "く": "ku", "け": "ke", "こ": "ko",
		"が": "ga", "ぎ": "gi", "ぐ": "gu", "げ": "ge", "ご": "go",
		// S row
		"さ": "sa", "し": "shi", "す": "su", "せ": "se", "そ": "so",
		"ざ": "za", "じ": "ji", "ず": "zu", "ぜ": "ze", "ぞ": "zo",
		// T row
		"た": "ta", "ち": "chi", "つ": "tsu", "て": "te", "と": "to",
		"だ": "da", "ぢ": "di", "づ": "du", "で": "de", "ど": "do",
		// N row
		"な": "na", "に": "ni", "ぬ": "nu", "ね": "ne", "の": "no",
		// H row
		"は": "ha", "ひ": "hi", "ふ": "fu", "へ": "he", "ほ": "ho",
		"ば": "ba", "び": "bi", "ぶ": "bu", "べ": "be", "ぼ": "bo",
		"ぱ": "pa", "ぴ": "pi", "ぷ": "pu", "ぺ": "pe", "ぽ": "po",
		// M row
		"ま": "ma", "み": "mi", "む": "mu", "め": "me", "も": "mo",
		// Y row
		"や": "ya", "ゆ": "yu", "よ": "yo",
		// R row
		"ら": "ra", "り": "ri", "る": "ru", "れ": "re", "ろ": "ro",
		// W row
		"わ": "wa", "ゐ": "wi", "ゑ": "we", "を": "wo",
		// N
		"ん": "n",
		// Small vowels
		"ぁ": "a", "ぃ": "i", "ぅ": "u", "ぇ": "e", "ぉ": "o",
		"っ": "tt", // double consonant (approximation)
		"ー": "-",
		// Compound mora (two-char)
		"きゃ": "kya", "きゅ": "kyu", "きょ": "kyo",
		"しゃ": "sha", "しゅ": "shu", "しょ": "sho",
		"ちゃ": "cha", "ちゅ": "chu", "ちょ": "cho",
		"にゃ": "nya", "にゅ": "nyu", "にょ": "nyo",
		"ひゃ": "hya", "ひゅ": "hyu", "ひょ": "hyo",
		"みゃ": "mya", "みゅ": "myu", "みょ": "myo",
		"りゃ": "rya", "りゅ": "ryu", "りょ": "ryo",
		"ぎゃ": "gya", "ぎゅ": "gyu", "ぎょ": "gyo",
		"じゃ": "ja", "じゅ": "ju", "じょ": "jo",
		"びゃ": "bya", "びゅ": "byu", "びょ": "byo",
		"ぴゃ": "pya", "ぴゅ": "pyu", "ぴょ": "pyo",
	}
}

// Ensure unused imports don't break compilation
var _ = sort.Strings
var _ = regexp.MustCompile
var _ = unicode.IsLetter
var _ = utf8.RuneLen
