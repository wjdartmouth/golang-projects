package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

//go:embed data/vocab/*.json data/kanji/*.json data/grammar/*.json
var dataFS embed.FS

//go:embed demo.html
var demoHTML []byte

// ─────────────────────────────────────────────
//  Types
// ─────────────────────────────────────────────

type JLPTLevel string

const (
	N5 JLPTLevel = "N5"
	N4 JLPTLevel = "N4"
	N3 JLPTLevel = "N3"
	N2 JLPTLevel = "N2"
	N1 JLPTLevel = "N1"
)

var allLevels = []JLPTLevel{N5, N4, N3, N2, N1}

type QuizKind string

const (
	KindVocab   QuizKind = "vocab"
	KindKanji   QuizKind = "kanji"
	KindGrammar QuizKind = "grammar"
)

type QuizMode string

const (
	// Vocab
	ModeKanjiToMeaning QuizMode = "kanji_to_meaning"
	ModeMeaningToKanji QuizMode = "meaning_to_kanji"
	ModeKanaToKanji    QuizMode = "kana_to_kanji"
	ModeFlashcard      QuizMode = "flashcard"
	// Kanji
	ModeCharToMeaning QuizMode = "character_to_meaning"
	ModeMeaningToChar QuizMode = "meaning_to_character"
	ModeCharToReading QuizMode = "character_to_reading"
	// Grammar
	ModePatternToMeaning QuizMode = "pattern_to_meaning"
	ModeMeaningToPattern QuizMode = "meaning_to_pattern"
)

func defaultMode(kind QuizKind) QuizMode {
	switch kind {
	case KindKanji:
		return ModeCharToMeaning
	case KindGrammar:
		return ModePatternToMeaning
	default:
		return ModeKanjiToMeaning
	}
}

func validMode(kind QuizKind, mode QuizMode) bool {
	switch kind {
	case KindVocab:
		return mode == ModeKanjiToMeaning || mode == ModeMeaningToKanji || mode == ModeKanaToKanji || mode == ModeFlashcard
	case KindKanji:
		return mode == ModeCharToMeaning || mode == ModeMeaningToChar || mode == ModeCharToReading
	case KindGrammar:
		return mode == ModePatternToMeaning || mode == ModeMeaningToPattern
	}
	return false
}

type Example struct {
	Ja       string `json:"ja"`
	Furigana string `json:"furigana,omitempty"`
	En       string `json:"en,omitempty"`
}

type Vocab struct {
	ID       string    `json:"id"`
	Word     string    `json:"word"`
	Reading  string    `json:"reading"`
	Romaji   string    `json:"romaji"`
	Meanings []string  `json:"meanings"`
	Level    JLPTLevel `json:"level"`
	Pos      []string  `json:"pos,omitempty"`
	Example  *Example  `json:"example,omitempty"`
}

func (v Vocab) MeaningText() string {
	return strings.Join(v.Meanings, "; ")
}

func (v Vocab) HasKanji() bool {
	return hasKanji(v.Word)
}

type Kanji struct {
	Character string    `json:"character"`
	Level     JLPTLevel `json:"level"`
	Strokes   int       `json:"strokes"`
	Onyomi    []string  `json:"onyomi,omitempty"`
	Kunyomi   []string  `json:"kunyomi,omitempty"`
	Meanings  []string  `json:"meanings"`
	Words     []string  `json:"words,omitempty"`
}

func (k Kanji) ID() string { return k.Character }

func (k Kanji) MeaningText() string {
	return strings.Join(k.Meanings, "; ")
}

func (k Kanji) ReadingText() string {
	on := strings.Join(k.Onyomi, "・")
	kun := strings.Join(k.Kunyomi, "・")
	switch {
	case on != "" && kun != "":
		return on + " / " + kun
	case on != "":
		return on
	default:
		return kun
	}
}

type Grammar struct {
	ID        string    `json:"id"`
	Pattern   string    `json:"pattern"`
	Romaji    string    `json:"romaji"`
	Level     JLPTLevel `json:"level"`
	Meaning   string    `json:"meaning"`
	Formation string    `json:"formation"`
	Notes     string    `json:"notes,omitempty"`
	Examples  []Example `json:"examples,omitempty"`
}

func (g Grammar) FirstExample() *Example {
	if len(g.Examples) == 0 {
		return nil
	}
	for i := range g.Examples {
		if g.Examples[i].Furigana != "" {
			return &g.Examples[i]
		}
	}
	return &g.Examples[0]
}

type QuizSession struct {
	ID          string
	Level       JLPTLevel
	Kind        QuizKind
	Mode        QuizMode
	ItemIDs     []string
	CurrentIdx  int
	Score       int
	Total       int
	Correct     int
	Wrong       int
	StartedAt   time.Time
	UpdatedAt   time.Time
	Finished    bool
	Misses      []Miss
	StreakCount int
	BestStreak  int
	ChoiceKey   map[string]string // opaque choice token → item id (current question)
}

type QuizQuestion struct {
	SessionID   string   `json:"session_id"`
	QuestionNum int      `json:"question_num"`
	Total       int      `json:"total"`
	Kind        QuizKind `json:"kind"`
	Mode        QuizMode `json:"mode"`
	ItemID      string   `json:"item_id"`
	WordID      string   `json:"word_id"` // backward compatible alias of item_id
	Prompt      string   `json:"prompt"`
	PromptSub   string   `json:"prompt_sub,omitempty"`
	Choices     []Choice `json:"choices,omitempty"`
	Answer      string   `json:"answer,omitempty"`     // flashcard only
	AnswerSub   string   `json:"answer_sub,omitempty"` // flashcard only
	Score       int      `json:"score"`
	Correct     int      `json:"correct"`
	Wrong       int      `json:"wrong"`
	Streak      int      `json:"streak"`
}

type Choice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Sub  string `json:"sub,omitempty"`
}

type AnswerRequest struct {
	SessionID string `json:"session_id"`
	ItemID    string `json:"item_id"`
	WordID    string `json:"word_id"`
	ChoiceID  string `json:"choice_id"`
	Correct   *bool  `json:"correct"` // flashcard self-assess
}

type AnswerResult struct {
	Correct         bool          `json:"correct"`
	CorrectAnswer   string        `json:"correct_answer"`
	CorrectSub      string        `json:"correct_sub,omitempty"`
	Explanation     string        `json:"explanation,omitempty"`
	ExampleJa       string        `json:"example_ja,omitempty"`
	ExampleFurigana string        `json:"example_furigana,omitempty"`
	ExampleEn       string        `json:"example_en,omitempty"`
	Formation       string        `json:"formation,omitempty"`
	Notes           string        `json:"notes,omitempty"`
	Score           int           `json:"score"`
	CorrectCount    int           `json:"correct_count"`
	WrongCount      int           `json:"wrong_count"`
	Streak          int           `json:"streak"`
	SessionFinished bool          `json:"session_finished"`
	CorrectChoiceID string        `json:"correct_choice_id,omitempty"`
	NextQuestion    *QuizQuestion `json:"next_question,omitempty"`
}

type Miss struct {
	ID      string   `json:"id"`
	Kind    QuizKind `json:"kind"`
	Prompt  string   `json:"prompt"`
	Answer  string   `json:"answer"`
	Reading string   `json:"reading,omitempty"`
}

type SessionSummary struct {
	SessionID  string    `json:"session_id"`
	Level      JLPTLevel `json:"level"`
	Kind       QuizKind  `json:"kind"`
	Mode       QuizMode  `json:"mode"`
	Total      int       `json:"total"`
	Correct    int       `json:"correct"`
	Wrong      int       `json:"wrong"`
	Score      int       `json:"score"`
	Accuracy   float64   `json:"accuracy_pct"`
	BestStreak int       `json:"best_streak"`
	Duration   string    `json:"duration"`
	Grade      string    `json:"grade"`
	Misses     []Miss    `json:"misses"`
	WrongWords []Vocab   `json:"wrong_words,omitempty"`
	StartedAt  time.Time `json:"started_at"`
}

type choiceSrc struct {
	id, text, sub string
}

// ─────────────────────────────────────────────
//  In-memory stores
// ─────────────────────────────────────────────

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*QuizSession
}

var store = &SessionStore{sessions: make(map[string]*QuizSession)}

func (s *SessionStore) Get(id string) (*QuizSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *SessionStore) Set(sess *QuizSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
}

func (s *SessionStore) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

var (
	allVocab    []Vocab
	allKanji    []Kanji
	allGrammar  []Grammar
	vocabByID   = map[string]Vocab{}
	kanjiByID   = map[string]Kanji{}
	grammarByID = map[string]Grammar{}
)

// ─────────────────────────────────────────────
//  Data loading
// ─────────────────────────────────────────────

func loadData() {
	var vocab, kanji, grammar int
	for _, lv := range []string{"n5", "n4", "n3", "n2", "n1"} {
		v := mustLoad[[]Vocab](fmt.Sprintf("data/vocab/%s.json", lv))
		for _, item := range v {
			if item.ID == "" || item.Word == "" {
				continue
			}
			allVocab = append(allVocab, item)
			vocabByID[item.ID] = item
		}
		vocab += len(v)

		k := mustLoad[[]Kanji](fmt.Sprintf("data/kanji/%s.json", lv))
		for _, item := range k {
			if item.Character == "" {
				continue
			}
			allKanji = append(allKanji, item)
			kanjiByID[item.Character] = item
		}
		kanji += len(k)

		g := mustLoad[[]Grammar](fmt.Sprintf("data/grammar/%s.json", lv))
		for _, item := range g {
			if item.ID == "" {
				continue
			}
			allGrammar = append(allGrammar, item)
			grammarByID[item.ID] = item
		}
		grammar += len(g)
	}
	if vocab == 0 || kanji == 0 || grammar == 0 {
		log.Fatalf("OpenJLPT data loaded empty (vocab=%d kanji=%d grammar=%d)", vocab, kanji, grammar)
	}
	log.Printf("OpenJLPT data loaded: %d vocab, %d kanji, %d grammar", vocab, kanji, grammar)
}

func mustLoad[T any](path string) T {
	var zero T
	b, err := dataFS.ReadFile(path)
	if err != nil {
		log.Fatalf("required OpenJLPT data file missing or unreadable: %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		log.Fatalf("required OpenJLPT data file is invalid JSON: %s: %v", path, err)
	}
	switch items := any(v).(type) {
	case []Vocab:
		if len(items) == 0 {
			log.Fatalf("required OpenJLPT data file is empty: %s", path)
		}
	case []Kanji:
		if len(items) == 0 {
			log.Fatalf("required OpenJLPT data file is empty: %s", path)
		}
	case []Grammar:
		if len(items) == 0 {
			log.Fatalf("required OpenJLPT data file is empty: %s", path)
		}
	}
	_ = zero
	return v
}

func hasKanji(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func normalizeLevel(s string) JLPTLevel {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch JLPTLevel(s) {
	case N5, N4, N3, N2, N1:
		return JLPTLevel(s)
	}
	return ""
}

func filterVocab(level JLPTLevel, pos string) []Vocab {
	var out []Vocab
	for _, w := range allVocab {
		if level != "" && w.Level != level {
			continue
		}
		if pos != "" {
			ok := false
			for _, p := range w.Pos {
				if p == pos {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		out = append(out, w)
	}
	return out
}

func filterKanji(level JLPTLevel) []Kanji {
	var out []Kanji
	for _, k := range allKanji {
		if level != "" && k.Level != level {
			continue
		}
		out = append(out, k)
	}
	return out
}

func filterGrammar(level JLPTLevel) []Grammar {
	var out []Grammar
	for _, g := range allGrammar {
		if level != "" && g.Level != level {
			continue
		}
		out = append(out, g)
	}
	return out
}

func shuffle[T any](in []T) []T {
	cp := make([]T, len(in))
	copy(cp, in)
	rand.Shuffle(len(cp), func(i, j int) { cp[i], cp[j] = cp[j], cp[i] })
	return cp
}

// ─────────────────────────────────────────────
//  Quiz engine
// ─────────────────────────────────────────────

func vocabPoolForMode(level JLPTLevel, mode QuizMode) []Vocab {
	pool := filterVocab(level, "")
	if mode == ModeKanaToKanji || mode == ModeMeaningToKanji {
		var withKanji []Vocab
		for _, w := range pool {
			if w.HasKanji() {
				withKanji = append(withKanji, w)
			}
		}
		if len(withKanji) >= 4 {
			return withKanji
		}
	}
	return pool
}

func buildQuestion(sess *QuizSession) *QuizQuestion {
	if sess.CurrentIdx >= len(sess.ItemIDs) {
		return nil
	}
	id := sess.ItemIDs[sess.CurrentIdx]
	q := &QuizQuestion{
		SessionID:   sess.ID,
		QuestionNum: sess.CurrentIdx + 1,
		Total:       sess.Total,
		Kind:        sess.Kind,
		Mode:        sess.Mode,
		ItemID:      id,
		WordID:      id,
		Score:       sess.Score,
		Correct:     sess.Correct,
		Wrong:       sess.Wrong,
		Streak:      sess.StreakCount,
	}

	switch sess.Kind {
	case KindVocab:
		buildVocabQuestion(q, sess, id)
	case KindKanji:
		buildKanjiQuestion(q, sess, id)
	case KindGrammar:
		buildGrammarQuestion(q, sess, id)
	}
	return q
}

func buildVocabQuestion(q *QuizQuestion, sess *QuizSession, id string) {
	word, ok := vocabByID[id]
	if !ok {
		return
	}
	switch sess.Mode {
	case ModeKanjiToMeaning:
		q.Prompt = word.Word
		q.PromptSub = word.Reading
		q.Choices = buildChoices(sess, choiceSrc{id: word.ID, text: word.MeaningText()}, vocabMeaningChoices(sess.ItemIDs), vocabMeaningChoices(vocabIDs(allVocab)))
	case ModeMeaningToKanji:
		q.Prompt = word.MeaningText()
		if word.HasKanji() {
			q.Choices = buildChoices(sess, choiceSrc{id: word.ID, text: word.Word, sub: word.Reading}, vocabWordChoices(sess.ItemIDs, true), vocabWordChoices(vocabIDs(allVocab), true))
		} else {
			q.Choices = buildChoices(sess, choiceSrc{id: word.ID, text: word.Word, sub: word.Romaji}, vocabWordChoices(sess.ItemIDs, false), vocabWordChoices(vocabIDs(allVocab), false))
		}
	case ModeKanaToKanji:
		q.Prompt = word.Reading
		q.PromptSub = word.Romaji
		if word.HasKanji() {
			q.Choices = buildChoices(sess, choiceSrc{id: word.ID, text: word.Word, sub: word.Reading}, vocabWordChoices(sess.ItemIDs, true), vocabWordChoices(vocabIDs(allVocab), true))
		} else {
			q.Choices = buildChoices(sess, choiceSrc{id: word.ID, text: word.MeaningText()}, vocabMeaningChoices(sess.ItemIDs), vocabMeaningChoices(vocabIDs(allVocab)))
		}
	case ModeFlashcard:
		q.Prompt = word.Word
		q.PromptSub = word.Reading
		q.Answer = word.MeaningText()
		if word.Romaji != "" {
			q.AnswerSub = word.Romaji
		}
	}
}

func buildKanjiQuestion(q *QuizQuestion, sess *QuizSession, id string) {
	k, ok := kanjiByID[id]
	if !ok {
		return
	}
	switch sess.Mode {
	case ModeCharToMeaning:
		q.Prompt = k.Character
		if k.Strokes > 0 {
			q.PromptSub = fmt.Sprintf("%d strokes", k.Strokes)
		}
		q.Choices = buildChoices(sess, choiceSrc{id: k.Character, text: k.MeaningText()}, kanjiMeaningChoices(sess.ItemIDs), kanjiMeaningChoices(kanjiIDs(allKanji)))
	case ModeMeaningToChar:
		q.Prompt = k.MeaningText()
		q.Choices = buildChoices(sess, choiceSrc{id: k.Character, text: k.Character, sub: k.ReadingText()}, kanjiCharChoices(sess.ItemIDs), kanjiCharChoices(kanjiIDs(allKanji)))
	case ModeCharToReading:
		q.Prompt = k.Character
		q.PromptSub = k.MeaningText()
		q.Choices = buildChoices(sess, choiceSrc{id: k.Character, text: k.ReadingText()}, kanjiReadingChoices(sess.ItemIDs), kanjiReadingChoices(kanjiIDs(allKanji)))
	}
}

func buildGrammarQuestion(q *QuizQuestion, sess *QuizSession, id string) {
	g, ok := grammarByID[id]
	if !ok {
		return
	}
	switch sess.Mode {
	case ModePatternToMeaning:
		q.Prompt = g.Pattern
		q.PromptSub = g.Romaji
		q.Choices = buildChoices(sess, choiceSrc{id: g.ID, text: g.Meaning}, grammarMeaningChoices(sess.ItemIDs), grammarMeaningChoices(grammarIDs(allGrammar)))
	case ModeMeaningToPattern:
		q.Prompt = g.Meaning
		q.Choices = buildChoices(sess, choiceSrc{id: g.ID, text: g.Pattern, sub: g.Romaji}, grammarPatternChoices(sess.ItemIDs), grammarPatternChoices(grammarIDs(allGrammar)))
	}
}

func vocabIDs(items []Vocab) []string {
	ids := make([]string, len(items))
	for i, w := range items {
		ids[i] = w.ID
	}
	return ids
}
func kanjiIDs(items []Kanji) []string {
	ids := make([]string, len(items))
	for i, k := range items {
		ids[i] = k.Character
	}
	return ids
}
func grammarIDs(items []Grammar) []string {
	ids := make([]string, len(items))
	for i, g := range items {
		ids[i] = g.ID
	}
	return ids
}

func vocabMeaningChoices(ids []string) []choiceSrc {
	out := make([]choiceSrc, 0, len(ids))
	for _, id := range ids {
		if w, ok := vocabByID[id]; ok && w.MeaningText() != "" {
			out = append(out, choiceSrc{id: w.ID, text: w.MeaningText()})
		}
	}
	return out
}
func vocabWordChoices(ids []string, requireKanji bool) []choiceSrc {
	out := make([]choiceSrc, 0, len(ids))
	for _, id := range ids {
		w, ok := vocabByID[id]
		if !ok || w.Word == "" {
			continue
		}
		if requireKanji && !w.HasKanji() {
			continue
		}
		out = append(out, choiceSrc{id: w.ID, text: w.Word, sub: w.Reading})
	}
	return out
}
func kanjiMeaningChoices(ids []string) []choiceSrc {
	out := make([]choiceSrc, 0, len(ids))
	for _, id := range ids {
		if k, ok := kanjiByID[id]; ok && k.MeaningText() != "" {
			out = append(out, choiceSrc{id: k.Character, text: k.MeaningText()})
		}
	}
	return out
}
func kanjiCharChoices(ids []string) []choiceSrc {
	out := make([]choiceSrc, 0, len(ids))
	for _, id := range ids {
		if k, ok := kanjiByID[id]; ok {
			out = append(out, choiceSrc{id: k.Character, text: k.Character, sub: k.ReadingText()})
		}
	}
	return out
}
func kanjiReadingChoices(ids []string) []choiceSrc {
	out := make([]choiceSrc, 0, len(ids))
	for _, id := range ids {
		if k, ok := kanjiByID[id]; ok && k.ReadingText() != "" {
			out = append(out, choiceSrc{id: k.Character, text: k.ReadingText()})
		}
	}
	return out
}
func grammarMeaningChoices(ids []string) []choiceSrc {
	out := make([]choiceSrc, 0, len(ids))
	for _, id := range ids {
		if g, ok := grammarByID[id]; ok && g.Meaning != "" {
			out = append(out, choiceSrc{id: g.ID, text: g.Meaning})
		}
	}
	return out
}
func grammarPatternChoices(ids []string) []choiceSrc {
	out := make([]choiceSrc, 0, len(ids))
	for _, id := range ids {
		if g, ok := grammarByID[id]; ok {
			out = append(out, choiceSrc{id: g.ID, text: g.Pattern, sub: g.Romaji})
		}
	}
	return out
}

func buildChoices(sess *QuizSession, correct choiceSrc, near, fallback []choiceSrc) []Choice {
	correctChoice := Choice{ID: correct.id, Text: correct.text, Sub: correct.sub}
	distractors := make([]Choice, 0, 3)
	used := map[string]bool{correct.text: true}

	addFrom := func(pool []choiceSrc) {
		for _, src := range shuffle(pool) {
			if len(distractors) == 3 {
				return
			}
			if src.id == correct.id || src.text == "" || used[src.text] {
				continue
			}
			used[src.text] = true
			distractors = append(distractors, Choice{ID: src.id, Text: src.text, Sub: src.sub})
		}
	}
	addFrom(near)
	if len(distractors) < 3 {
		addFrom(fallback)
	}

	choices := append(distractors, correctChoice)
	rand.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })

	// Opaque tokens so clients cannot match choice id to the item id.
	sess.ChoiceKey = map[string]string{}
	for i := range choices {
		tok := fmt.Sprintf("c%x", rand.Int63())
		sess.ChoiceKey[tok] = choices[i].ID
		choices[i].ID = tok
	}
	return choices
}

func gradeSession(sess *QuizSession) string {
	if sess.Total == 0 {
		return "N/A"
	}
	pct := float64(sess.Correct) / float64(sess.Total) * 100
	switch {
	case pct >= 95:
		return "S（優秀）"
	case pct >= 80:
		return "A（良い）"
	case pct >= 65:
		return "B（普通）"
	case pct >= 50:
		return "C（要復習）"
	default:
		return "D（要勉強）"
	}
}

func scoreForAnswer(isCorrect bool, streak int) int {
	if !isCorrect {
		return 0
	}
	base := 10
	if streak >= 5 {
		return base * 3
	}
	if streak >= 3 {
		return base * 2
	}
	return base
}

func revealFor(sess *QuizSession, id string) (answer, sub, explanation, formation, notes string, ex *Example) {
	switch sess.Kind {
	case KindVocab:
		w := vocabByID[id]
		answer, sub = w.MeaningText(), w.Reading
		if sess.Mode == ModeMeaningToKanji || sess.Mode == ModeKanaToKanji {
			answer, sub = w.Word, w.Reading
		}
		return answer, sub, "", "", "", w.Example
	case KindKanji:
		k := kanjiByID[id]
		switch sess.Mode {
		case ModeCharToReading:
			answer, sub = k.ReadingText(), k.MeaningText()
		case ModeMeaningToChar:
			answer, sub = k.Character, k.ReadingText()
		default:
			answer, sub = k.MeaningText(), k.ReadingText()
		}
		if len(k.Words) > 0 {
			explanation = "例: " + strings.Join(k.Words, "、")
		}
		return answer, sub, explanation, "", "", nil
	case KindGrammar:
		g := grammarByID[id]
		if sess.Mode == ModeMeaningToPattern {
			answer, sub = g.Pattern, g.Romaji
		} else {
			answer, sub = g.Meaning, g.Pattern
		}
		return answer, sub, "", g.Formation, g.Notes, g.FirstExample()
	}
	return "", "", "", "", "", nil
}

func missFor(sess *QuizSession, id string) Miss {
	m := Miss{ID: id, Kind: sess.Kind}
	switch sess.Kind {
	case KindVocab:
		w := vocabByID[id]
		m.Prompt, m.Answer, m.Reading = w.Word, w.MeaningText(), w.Reading
	case KindKanji:
		k := kanjiByID[id]
		m.Prompt, m.Answer, m.Reading = k.Character, k.MeaningText(), k.ReadingText()
	case KindGrammar:
		g := grammarByID[id]
		m.Prompt, m.Answer = g.Pattern, g.Meaning
	}
	return m
}

// ─────────────────────────────────────────────
//  HTTP
// ─────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func handleVocabulary(w http.ResponseWriter, r *http.Request) {
	level := normalizeLevel(r.URL.Query().Get("level"))
	pos := r.URL.Query().Get("category")
	if pos == "" {
		pos = r.URL.Query().Get("pos")
	}
	words := filterVocab(level, pos)
	rank := map[JLPTLevel]int{N5: 0, N4: 1, N3: 2, N2: 3, N1: 4}
	sort.Slice(words, func(i, j int) bool {
		if words[i].Level != words[j].Level {
			return rank[words[i].Level] < rank[words[j].Level]
		}
		return words[i].Word < words[j].Word
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": len(words),
		"words": words,
		"filters": map[string]string{
			"level": string(level),
			"pos":   pos,
		},
	})
}

func handleVocabularyOne(w http.ResponseWriter, r *http.Request, id string) {
	word, ok := vocabByID[id]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "単語が見つかりません: "+id)
		return
	}
	writeJSON(w, http.StatusOK, word)
}

func handleRandom(w http.ResponseWriter, r *http.Request) {
	level := normalizeLevel(r.URL.Query().Get("level"))
	pos := r.URL.Query().Get("category")
	if pos == "" {
		pos = r.URL.Query().Get("pos")
	}
	count := 10
	if n, err := strconv.Atoi(r.URL.Query().Get("count")); err == nil && n > 0 && n <= 50 {
		count = n
	}
	pool := filterVocab(level, pos)
	if len(pool) == 0 {
		writeError(w, http.StatusNotFound, "no_words", "該当する単語がありません")
		return
	}
	shuffled := shuffle(pool)
	if count > len(shuffled) {
		count = len(shuffled)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": count,
		"words": shuffled[:count],
	})
}

func handleQuizStart(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var req struct {
		Level JLPTLevel `json:"level"`
		Kind  QuizKind  `json:"kind"`
		Mode  QuizMode  `json:"mode"`
		Count int       `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "JSONの形式が正しくありません")
		return
	}

	if req.Level == "" {
		req.Level = N5
	}
	req.Level = normalizeLevel(string(req.Level))
	if req.Level == "" {
		writeError(w, http.StatusBadRequest, "invalid_level", "level は N5〜N1 で指定してください")
		return
	}
	if req.Kind == "" {
		req.Kind = KindVocab
	}
	req.Kind = QuizKind(strings.ToLower(string(req.Kind)))
	if req.Kind != KindVocab && req.Kind != KindKanji && req.Kind != KindGrammar {
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind は vocab, kanji, grammar のいずれかです")
		return
	}
	if req.Mode == "" {
		req.Mode = defaultMode(req.Kind)
	}
	if !validMode(req.Kind, req.Mode) {
		writeError(w, http.StatusBadRequest, "invalid_mode", "この kind では使えない mode です")
		return
	}
	if req.Count <= 0 || req.Count > 50 {
		req.Count = 10
	}

	var ids []string
	switch req.Kind {
	case KindVocab:
		pool := vocabPoolForMode(req.Level, req.Mode)
		if len(pool) == 0 {
			writeError(w, http.StatusBadRequest, "no_items", "指定レベル・種類の問題がありません")
			return
		}
		shuffled := shuffle(pool)
		if req.Count > len(shuffled) {
			req.Count = len(shuffled)
		}
		for _, w := range shuffled[:req.Count] {
			ids = append(ids, w.ID)
		}
	case KindKanji:
		pool := filterKanji(req.Level)
		if len(pool) == 0 {
			writeError(w, http.StatusBadRequest, "no_items", "指定レベルの漢字がありません")
			return
		}
		shuffled := shuffle(pool)
		if req.Count > len(shuffled) {
			req.Count = len(shuffled)
		}
		for _, k := range shuffled[:req.Count] {
			ids = append(ids, k.Character)
		}
	case KindGrammar:
		pool := filterGrammar(req.Level)
		if len(pool) == 0 {
			writeError(w, http.StatusBadRequest, "no_items", "指定レベルの文法がありません")
			return
		}
		shuffled := shuffle(pool)
		if req.Count > len(shuffled) {
			req.Count = len(shuffled)
		}
		for _, g := range shuffled[:req.Count] {
			ids = append(ids, g.ID)
		}
	}

	sess := &QuizSession{
		ID:        fmt.Sprintf("sess-%d", time.Now().UnixNano()),
		Level:     req.Level,
		Kind:      req.Kind,
		Mode:      req.Mode,
		ItemIDs:   ids,
		Total:     req.Count,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.Set(sess)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"session_id":     sess.ID,
		"level":          sess.Level,
		"kind":           sess.Kind,
		"mode":           sess.Mode,
		"total":          sess.Total,
		"first_question": buildQuestion(sess),
	})
}

func handleQuizAnswer(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var req AnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "JSONの形式が正しくありません")
		return
	}

	store.mu.Lock()
	sess, ok := store.sessions[req.SessionID]
	if !ok {
		store.mu.Unlock()
		writeError(w, http.StatusNotFound, "session_not_found", "セッションが見つかりません")
		return
	}
	if sess.Finished {
		store.mu.Unlock()
		writeError(w, http.StatusConflict, "session_finished", "このセッションは既に終了しています")
		return
	}

	itemID := req.ItemID
	if itemID == "" {
		itemID = req.WordID
	}
	if sess.CurrentIdx >= len(sess.ItemIDs) || sess.ItemIDs[sess.CurrentIdx] != itemID {
		store.mu.Unlock()
		writeError(w, http.StatusBadRequest, "item_mismatch", "この問題の item_id が一致しません")
		return
	}

	var isCorrect bool
	var correctTok string
	for tok, real := range sess.ChoiceKey {
		if real == itemID {
			correctTok = tok
			break
		}
	}
	if sess.Mode == ModeFlashcard {
		if req.Correct == nil {
			store.mu.Unlock()
			writeError(w, http.StatusBadRequest, "missing_correct", "フラッシュカードモードでは correct フィールドが必要です")
			return
		}
		isCorrect = *req.Correct
	} else {
		real, ok := sess.ChoiceKey[req.ChoiceID]
		if !ok || req.ChoiceID == "" {
			store.mu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_choice", "選択肢が無効です")
			return
		}
		isCorrect = real == itemID
	}

	if isCorrect {
		sess.Correct++
		sess.StreakCount++
		if sess.StreakCount > sess.BestStreak {
			sess.BestStreak = sess.StreakCount
		}
		sess.Score += scoreForAnswer(true, sess.StreakCount)
	} else {
		sess.Wrong++
		sess.StreakCount = 0
		sess.Misses = append(sess.Misses, missFor(sess, itemID))
	}
	sess.CurrentIdx++
	sess.UpdatedAt = time.Now()
	finished := sess.CurrentIdx >= sess.Total
	sess.Finished = finished

	answer, sub, explanation, formation, notes, ex := revealFor(sess, itemID)
	result := AnswerResult{
		Correct:         isCorrect,
		CorrectAnswer:   answer,
		CorrectSub:      sub,
		Explanation:     explanation,
		Formation:       formation,
		Notes:           notes,
		Score:           sess.Score,
		CorrectCount:    sess.Correct,
		WrongCount:      sess.Wrong,
		Streak:          sess.StreakCount,
		SessionFinished: finished,
		CorrectChoiceID: correctTok,
	}
	if ex != nil {
		result.ExampleJa = ex.Ja
		result.ExampleFurigana = ex.Furigana
		result.ExampleEn = ex.En
	}
	if !finished {
		result.NextQuestion = buildQuestion(sess)
	}
	store.mu.Unlock()

	writeJSON(w, http.StatusOK, result)
}

func handleQuizSummary(w http.ResponseWriter, r *http.Request, id string) {
	sess, ok := store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "セッションが見つかりません")
		return
	}
	accuracy := 0.0
	if sess.Total > 0 {
		accuracy = float64(sess.Correct) / float64(sess.Total) * 100
	}
	summary := SessionSummary{
		SessionID:  sess.ID,
		Level:      sess.Level,
		Kind:       sess.Kind,
		Mode:       sess.Mode,
		Total:      sess.Total,
		Correct:    sess.Correct,
		Wrong:      sess.Wrong,
		Score:      sess.Score,
		Accuracy:   accuracy,
		BestStreak: sess.BestStreak,
		Duration:   time.Since(sess.StartedAt).Round(time.Second).String(),
		Grade:      gradeSession(sess),
		Misses:     sess.Misses,
		StartedAt:  sess.StartedAt,
	}
	if sess.Kind == KindVocab {
		for _, m := range sess.Misses {
			if w, ok := vocabByID[m.ID]; ok {
				summary.WrongWords = append(summary.WrongWords, w)
			}
		}
	}
	writeJSON(w, http.StatusOK, summary)
}

func handleQuizCurrent(w http.ResponseWriter, r *http.Request, id string) {
	sess, ok := store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "セッションが見つかりません")
		return
	}
	if sess.Finished {
		writeError(w, http.StatusGone, "session_finished", "クイズは終了しています。/summary を呼び出してください")
		return
	}
	writeJSON(w, http.StatusOK, buildQuestion(sess))
}

func countsByLevel() []map[string]interface{} {
	type c struct{ words, kanji, grammar int }
	tab := map[JLPTLevel]*c{}
	for _, lv := range allLevels {
		tab[lv] = &c{}
	}
	for _, w := range allVocab {
		if tab[w.Level] != nil {
			tab[w.Level].words++
		}
	}
	for _, k := range allKanji {
		if tab[k.Level] != nil {
			tab[k.Level].kanji++
		}
	}
	for _, g := range allGrammar {
		if tab[g.Level] != nil {
			tab[g.Level].grammar++
		}
	}
	out := make([]map[string]interface{}, 0, 5)
	for _, lv := range allLevels {
		out = append(out, map[string]interface{}{
			"level":         lv,
			"word_count":    tab[lv].words,
			"kanji_count":   tab[lv].kanji,
			"grammar_count": tab[lv].grammar,
		})
	}
	return out
}

func handleLevels(w http.ResponseWriter, r *http.Request) {
	levels := countsByLevel()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"levels":        levels,
		"total_words":   len(allVocab),
		"total_kanji":   len(allKanji),
		"total_grammar": len(allGrammar),
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_words":     len(allVocab),
		"total_kanji":     len(allKanji),
		"total_grammar":   len(allGrammar),
		"active_sessions": store.ActiveCount(),
		"by_level":        countsByLevel(),
		"kinds":           []string{string(KindVocab), string(KindKanji), string(KindGrammar)},
		"modes": map[string][]string{
			"vocab":   {string(ModeKanjiToMeaning), string(ModeMeaningToKanji), string(ModeKanaToKanji), string(ModeFlashcard)},
			"kanji":   {string(ModeCharToMeaning), string(ModeMeaningToChar), string(ModeCharToReading)},
			"grammar": {string(ModePatternToMeaning), string(ModeMeaningToPattern)},
		},
		"data_source": "OpenJLPT (https://github.com/evanclan/OpenJLPT), CC BY-SA 4.0",
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"service": "JLPTクイズAPI",
		"version": "2.0.0",
		"words":   len(allVocab),
		"kanji":   len(allKanji),
		"grammar": len(allGrammar),
	})
}

func handleDemo(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" && r.URL.Path != "/demo.html" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(demoHTML)
}

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next(w, r)
		log.Printf("[%s] %s %s — %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	}
}

func router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/vocabulary", loggingMiddleware(handleVocabulary))
	mux.HandleFunc("/api/v1/vocabulary/random", loggingMiddleware(handleRandom))
	mux.HandleFunc("/api/v1/vocabulary/", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/vocabulary/")
		if id == "" {
			handleVocabulary(w, r)
			return
		}
		handleVocabularyOne(w, r, id)
	}))

	mux.HandleFunc("/api/v1/quiz/start", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodOptions {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST メソッドのみ対応")
			return
		}
		handleQuizStart(w, r)
	}))
	mux.HandleFunc("/api/v1/quiz/answer", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodOptions {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST メソッドのみ対応")
			return
		}
		handleQuizAnswer(w, r)
	}))
	mux.HandleFunc("/api/v1/quiz/", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/quiz/")
		parts := strings.Split(path, "/")
		if len(parts) == 1 && parts[0] != "" {
			handleQuizCurrent(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "summary" {
			handleQuizSummary(w, r, parts[0])
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/levels", loggingMiddleware(handleLevels))
	mux.HandleFunc("/api/v1/stats", loggingMiddleware(handleStats))
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", handleDemo)

	return mux
}

func main() {
	rand.Seed(time.Now().UnixNano())
	loadData()

	if _, err := fs.Stat(dataFS, "data/vocab/n5.json"); err != nil {
		log.Fatalf("embedded data check failed: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}
	addr := "0.0.0.0:" + port
	log.Printf("JLPTクイズAPI 起動中: http://localhost:%s  (vocab=%d kanji=%d grammar=%d)", port, len(allVocab), len(allKanji), len(allGrammar))
	if err := http.ListenAndServe(addr, router()); err != nil {
		log.Fatal(err)
	}
}
