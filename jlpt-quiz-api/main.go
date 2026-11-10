package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────
//  Data Models
// ─────────────────────────────────────────────

// JLPTLevel represents one of N5–N1
type JLPTLevel string

const (
	N5 JLPTLevel = "N5"
	N4 JLPTLevel = "N4"
	N3 JLPTLevel = "N3"
	N2 JLPTLevel = "N2"
	N1 JLPTLevel = "N1"
)

// WordCategory groups vocabulary thematically
type WordCategory string

const (
	CatTime     WordCategory = "時間・日付"
	CatNumbers  WordCategory = "数・量"
	CatFamily   WordCategory = "家族・人"
	CatBody     WordCategory = "体・健康"
	CatFood     WordCategory = "食べ物・飲み物"
	CatTransport WordCategory = "交通・移動"
	CatWork     WordCategory = "仕事・学校"
	CatNature   WordCategory = "自然・天気"
	CatHome     WordCategory = "家・生活"
	CatEmotion  WordCategory = "感情・状態"
	CatVerbs    WordCategory = "動詞"
	CatAdj      WordCategory = "形容詞"
	CatSociety  WordCategory = "社会・文化"
	CatAbstract WordCategory = "抽象・概念"
)

// Word is a single vocabulary entry
type Word struct {
	ID         string       `json:"id"`
	Kanji      string       `json:"kanji"`       // 日本語 (may be empty for kana-only)
	Kana       string       `json:"kana"`        // ひらがな・カタカナ
	Romaji     string       `json:"romaji"`      // romanization
	Meaning    string       `json:"meaning"`     // English meaning
	MeaningJa  string       `json:"meaning_ja"`  // Japanese meaning/example
	Level      JLPTLevel    `json:"level"`
	Category   WordCategory `json:"category"`
	ExampleJa  string       `json:"example_ja,omitempty"`
	ExampleEn  string       `json:"example_en,omitempty"`
	Tags       []string     `json:"tags,omitempty"`
}

// ─────────────────────────────────────────────
//  Quiz Session (in-memory, per-session state)
// ─────────────────────────────────────────────

// QuizSession tracks a user's ongoing quiz
type QuizSession struct {
	ID          string       `json:"id"`
	Level       JLPTLevel    `json:"level"`
	Category    WordCategory `json:"category,omitempty"`
	Mode        QuizMode     `json:"mode"`
	Words       []Word       `json:"words"`      // shuffled word pool
	CurrentIdx  int          `json:"current_idx"`
	Score       int          `json:"score"`
	Total       int          `json:"total"`
	Correct     int          `json:"correct"`
	Wrong       int          `json:"wrong"`
	StartedAt   time.Time    `json:"started_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Finished    bool         `json:"finished"`
	WrongWords  []Word       `json:"wrong_words"`
	StreakCount int          `json:"streak_count"`
	BestStreak  int          `json:"best_streak"`
}

// QuizMode determines question format
type QuizMode string

const (
	ModeKanjiToMeaning QuizMode = "kanji_to_meaning" // see kanji → pick meaning
	ModeMeaningToKanji QuizMode = "meaning_to_kanji" // see meaning → pick kanji
	ModeKanaToKanji    QuizMode = "kana_to_kanji"    // see kana → pick kanji
	ModeFlashcard      QuizMode = "flashcard"        // flip-style, self-assess
)

// QuizQuestion is sent to client per question
type QuizQuestion struct {
	SessionID   string   `json:"session_id"`
	QuestionNum int      `json:"question_num"`
	Total       int      `json:"total"`
	Mode        QuizMode `json:"mode"`

	// The word being tested
	WordID   string `json:"word_id"`
	Prompt   string `json:"prompt"`    // what the user sees
	PromptSub string `json:"prompt_sub,omitempty"` // secondary hint

	// Multiple choice options (shuffled, includes correct)
	Choices []Choice `json:"choices"`

	// For flashcard mode
	Answer     string `json:"answer,omitempty"`
	AnswerSub  string `json:"answer_sub,omitempty"`

	// Progress
	Score      int `json:"score"`
	Correct    int `json:"correct"`
	Wrong      int `json:"wrong"`
	Streak     int `json:"streak"`
}

// Choice is one multiple-choice option
type Choice struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Sub     string `json:"sub,omitempty"`
	Correct bool   `json:"correct"`
}

// AnswerRequest from client
type AnswerRequest struct {
	SessionID string `json:"session_id"`
	WordID    string `json:"word_id"`
	ChoiceID  string `json:"choice_id"` // for MC
	Correct   *bool  `json:"correct"`   // for flashcard self-assess
}

// AnswerResult returned to client
type AnswerResult struct {
	Correct        bool   `json:"correct"`
	CorrectAnswer  string `json:"correct_answer"`
	CorrectSub     string `json:"correct_sub,omitempty"`
	Explanation    string `json:"explanation,omitempty"`
	ExampleJa      string `json:"example_ja,omitempty"`
	ExampleEn      string `json:"example_en,omitempty"`
	Score          int    `json:"score"`
	CorrectCount   int    `json:"correct_count"`
	WrongCount     int    `json:"wrong_count"`
	Streak         int    `json:"streak"`
	SessionFinished bool  `json:"session_finished"`
	NextQuestion   *QuizQuestion `json:"next_question,omitempty"`
}

// SessionSummary returned when quiz ends
type SessionSummary struct {
	SessionID   string    `json:"session_id"`
	Level       JLPTLevel `json:"level"`
	Mode        QuizMode  `json:"mode"`
	Total       int       `json:"total"`
	Correct     int       `json:"correct"`
	Wrong       int       `json:"wrong"`
	Score       int       `json:"score"`
	Accuracy    float64   `json:"accuracy_pct"`
	BestStreak  int       `json:"best_streak"`
	Duration    string    `json:"duration"`
	Grade       string    `json:"grade"`
	WrongWords  []Word    `json:"wrong_words"`
	StartedAt   time.Time `json:"started_at"`
}

// ─────────────────────────────────────────────
//  In-Memory Session Store
// ─────────────────────────────────────────────

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*QuizSession
}

var store = &SessionStore{
	sessions: make(map[string]*QuizSession),
}

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

func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *SessionStore) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// ─────────────────────────────────────────────
//  Vocabulary Database
// ─────────────────────────────────────────────

var vocabulary []Word

func initVocabulary() {
	vocabulary = []Word{
		// ── N5 ──────────────────────────────────────────────────────────
		// Time & Dates
		{ID: "n5-001", Kanji: "今日", Kana: "きょう", Romaji: "kyou", Meaning: "today", MeaningJa: "本日", Level: N5, Category: CatTime, ExampleJa: "今日は月曜日です。", ExampleEn: "Today is Monday."},
		{ID: "n5-002", Kanji: "明日", Kana: "あした", Romaji: "ashita", Meaning: "tomorrow", MeaningJa: "翌日", Level: N5, Category: CatTime, ExampleJa: "明日、学校に行きます。", ExampleEn: "I will go to school tomorrow."},
		{ID: "n5-003", Kanji: "昨日", Kana: "きのう", Romaji: "kinou", Meaning: "yesterday", MeaningJa: "前日", Level: N5, Category: CatTime},
		{ID: "n5-004", Kanji: "毎日", Kana: "まいにち", Romaji: "mainichi", Meaning: "every day", MeaningJa: "日々", Level: N5, Category: CatTime},
		{ID: "n5-005", Kanji: "今年", Kana: "ことし", Romaji: "kotoshi", Meaning: "this year", MeaningJa: "本年", Level: N5, Category: CatTime},
		{ID: "n5-006", Kanji: "来年", Kana: "らいねん", Romaji: "rainen", Meaning: "next year", MeaningJa: "翌年", Level: N5, Category: CatTime},
		// Numbers
		{ID: "n5-007", Kanji: "百", Kana: "ひゃく", Romaji: "hyaku", Meaning: "hundred", MeaningJa: "100", Level: N5, Category: CatNumbers},
		{ID: "n5-008", Kanji: "千", Kana: "せん", Romaji: "sen", Meaning: "thousand", MeaningJa: "1,000", Level: N5, Category: CatNumbers},
		{ID: "n5-009", Kanji: "万", Kana: "まん", Romaji: "man", Meaning: "ten thousand", MeaningJa: "10,000", Level: N5, Category: CatNumbers},
		// Family
		{ID: "n5-010", Kanji: "母", Kana: "はは", Romaji: "haha", Meaning: "mother (own)", MeaningJa: "自分の母親", Level: N5, Category: CatFamily, ExampleJa: "母は料理が上手です。", ExampleEn: "My mother is a good cook."},
		{ID: "n5-011", Kanji: "父", Kana: "ちち", Romaji: "chichi", Meaning: "father (own)", MeaningJa: "自分の父親", Level: N5, Category: CatFamily},
		{ID: "n5-012", Kanji: "兄", Kana: "あに", Romaji: "ani", Meaning: "older brother (own)", MeaningJa: "自分の兄", Level: N5, Category: CatFamily},
		{ID: "n5-013", Kanji: "姉", Kana: "あね", Romaji: "ane", Meaning: "older sister (own)", MeaningJa: "自分の姉", Level: N5, Category: CatFamily},
		// Food
		{ID: "n5-014", Kanji: "水", Kana: "みず", Romaji: "mizu", Meaning: "water", MeaningJa: "水分", Level: N5, Category: CatFood},
		{ID: "n5-015", Kanji: "肉", Kana: "にく", Romaji: "niku", Meaning: "meat", MeaningJa: "肉類", Level: N5, Category: CatFood},
		{ID: "n5-016", Kanji: "魚", Kana: "さかな", Romaji: "sakana", Meaning: "fish", MeaningJa: "魚類", Level: N5, Category: CatFood},
		{ID: "n5-017", Kanji: "野菜", Kana: "やさい", Romaji: "yasai", Meaning: "vegetables", MeaningJa: "野菜類", Level: N5, Category: CatFood},
		// Verbs
		{ID: "n5-018", Kanji: "食べる", Kana: "たべる", Romaji: "taberu", Meaning: "to eat", MeaningJa: "食事する", Level: N5, Category: CatVerbs},
		{ID: "n5-019", Kanji: "飲む", Kana: "のむ", Romaji: "nomu", Meaning: "to drink", MeaningJa: "飲用する", Level: N5, Category: CatVerbs},
		{ID: "n5-020", Kanji: "行く", Kana: "いく", Romaji: "iku", Meaning: "to go", MeaningJa: "移動する", Level: N5, Category: CatVerbs},
		{ID: "n5-021", Kanji: "来る", Kana: "くる", Romaji: "kuru", Meaning: "to come", MeaningJa: "到来する", Level: N5, Category: CatVerbs},
		{ID: "n5-022", Kanji: "見る", Kana: "みる", Romaji: "miru", Meaning: "to see / watch", MeaningJa: "視る", Level: N5, Category: CatVerbs},
		{ID: "n5-023", Kanji: "聞く", Kana: "きく", Romaji: "kiku", Meaning: "to listen / ask", MeaningJa: "聴く・尋ねる", Level: N5, Category: CatVerbs},
		{ID: "n5-024", Kanji: "話す", Kana: "はなす", Romaji: "hanasu", Meaning: "to speak", MeaningJa: "会話する", Level: N5, Category: CatVerbs},
		{ID: "n5-025", Kanji: "書く", Kana: "かく", Romaji: "kaku", Meaning: "to write", MeaningJa: "記す", Level: N5, Category: CatVerbs},
		{ID: "n5-026", Kanji: "読む", Kana: "よむ", Romaji: "yomu", Meaning: "to read", MeaningJa: "読書する", Level: N5, Category: CatVerbs},
		{ID: "n5-027", Kanji: "買う", Kana: "かう", Romaji: "kau", Meaning: "to buy", MeaningJa: "購入する", Level: N5, Category: CatVerbs},
		{ID: "n5-028", Kanji: "売る", Kana: "うる", Romaji: "uru", Meaning: "to sell", MeaningJa: "販売する", Level: N5, Category: CatVerbs},
		// Adjectives
		{ID: "n5-029", Kanji: "大きい", Kana: "おおきい", Romaji: "ookii", Meaning: "big / large", MeaningJa: "大型の", Level: N5, Category: CatAdj},
		{ID: "n5-030", Kanji: "小さい", Kana: "ちいさい", Romaji: "chiisai", Meaning: "small / little", MeaningJa: "小型の", Level: N5, Category: CatAdj},
		{ID: "n5-031", Kanji: "新しい", Kana: "あたらしい", Romaji: "atarashii", Meaning: "new", MeaningJa: "新品の", Level: N5, Category: CatAdj},
		{ID: "n5-032", Kanji: "古い", Kana: "ふるい", Romaji: "furui", Meaning: "old (thing)", MeaningJa: "年月を経た", Level: N5, Category: CatAdj},
		{ID: "n5-033", Kanji: "高い", Kana: "たかい", Romaji: "takai", Meaning: "tall / expensive", MeaningJa: "高価・高さがある", Level: N5, Category: CatAdj},
		{ID: "n5-034", Kanji: "安い", Kana: "やすい", Romaji: "yasui", Meaning: "cheap / inexpensive", MeaningJa: "低価格の", Level: N5, Category: CatAdj},

		// ── N4 ──────────────────────────────────────────────────────────
		{ID: "n4-001", Kanji: "経験", Kana: "けいけん", Romaji: "keiken", Meaning: "experience", MeaningJa: "体験・実経験", Level: N4, Category: CatWork, ExampleJa: "仕事の経験が三年あります。", ExampleEn: "I have three years of work experience."},
		{ID: "n4-002", Kanji: "練習", Kana: "れんしゅう", Romaji: "renshuu", Meaning: "practice / drill", MeaningJa: "反復練習", Level: N4, Category: CatWork},
		{ID: "n4-003", Kanji: "準備", Kana: "じゅんび", Romaji: "junbi", Meaning: "preparation", MeaningJa: "用意・支度", Level: N4, Category: CatWork},
		{ID: "n4-004", Kanji: "連絡", Kana: "れんらく", Romaji: "renraku", Meaning: "contact / communication", MeaningJa: "通知・通信", Level: N4, Category: CatWork},
		{ID: "n4-005", Kanji: "予約", Kana: "よやく", Romaji: "yoyaku", Meaning: "reservation / booking", MeaningJa: "事前予約", Level: N4, Category: CatWork},
		{ID: "n4-006", Kanji: "運動", Kana: "うんどう", Romaji: "undou", Meaning: "exercise / sport", MeaningJa: "身体活動", Level: N4, Category: CatBody},
		{ID: "n4-007", Kanji: "病気", Kana: "びょうき", Romaji: "byouki", Meaning: "illness / sickness", MeaningJa: "疾患", Level: N4, Category: CatBody},
		{ID: "n4-008", Kanji: "薬", Kana: "くすり", Romaji: "kusuri", Meaning: "medicine / drug", MeaningJa: "医薬品", Level: N4, Category: CatBody},
		{ID: "n4-009", Kanji: "旅行", Kana: "りょこう", Romaji: "ryokou", Meaning: "travel / trip", MeaningJa: "旅・旅行", Level: N4, Category: CatTransport},
		{ID: "n4-010", Kanji: "出発", Kana: "しゅっぱつ", Romaji: "shuppatsu", Meaning: "departure", MeaningJa: "出発・旅立ち", Level: N4, Category: CatTransport},
		{ID: "n4-011", Kanji: "到着", Kana: "とうちゃく", Romaji: "touchaku", Meaning: "arrival", MeaningJa: "到達・着", Level: N4, Category: CatTransport},
		{ID: "n4-012", Kanji: "乗る", Kana: "のる", Romaji: "noru", Meaning: "to ride / board", MeaningJa: "乗車・搭乗する", Level: N4, Category: CatVerbs},
		{ID: "n4-013", Kanji: "降りる", Kana: "おりる", Romaji: "oriru", Meaning: "to get off / descend", MeaningJa: "降車する", Level: N4, Category: CatVerbs},
		{ID: "n4-014", Kanji: "変える", Kana: "かえる", Romaji: "kaeru", Meaning: "to change (something)", MeaningJa: "変更する", Level: N4, Category: CatVerbs},
		{ID: "n4-015", Kanji: "決める", Kana: "きめる", Romaji: "kimeru", Meaning: "to decide", MeaningJa: "決定する", Level: N4, Category: CatVerbs},
		{ID: "n4-016", Kanji: "続ける", Kana: "つづける", Romaji: "tsuzukeru", Meaning: "to continue", MeaningJa: "継続する", Level: N4, Category: CatVerbs},
		{ID: "n4-017", Kanji: "忙しい", Kana: "いそがしい", Romaji: "isogashii", Meaning: "busy", MeaningJa: "多忙な", Level: N4, Category: CatAdj},
		{ID: "n4-018", Kanji: "難しい", Kana: "むずかしい", Romaji: "muzukashii", Meaning: "difficult", MeaningJa: "困難な", Level: N4, Category: CatAdj},
		{ID: "n4-019", Kanji: "危ない", Kana: "あぶない", Romaji: "abunai", Meaning: "dangerous", MeaningJa: "危険な", Level: N4, Category: CatAdj},
		{ID: "n4-020", Kanji: "丁寧", Kana: "ていねい", Romaji: "teinei", Meaning: "polite / careful", MeaningJa: "礼儀正しい", Level: N4, Category: CatSociety},

		// ── N3 ──────────────────────────────────────────────────────────
		{ID: "n3-001", Kanji: "意見", Kana: "いけん", Romaji: "iken", Meaning: "opinion / view", MeaningJa: "考え・見解", Level: N3, Category: CatAbstract, ExampleJa: "あなたの意見を聞かせてください。", ExampleEn: "Please share your opinion."},
		{ID: "n3-002", Kanji: "理由", Kana: "りゆう", Romaji: "riyuu", Meaning: "reason / cause", MeaningJa: "原因・わけ", Level: N3, Category: CatAbstract},
		{ID: "n3-003", Kanji: "結果", Kana: "けっか", Romaji: "kekka", Meaning: "result / outcome", MeaningJa: "成果・結末", Level: N3, Category: CatAbstract},
		{ID: "n3-004", Kanji: "影響", Kana: "えいきょう", Romaji: "eikyou", Meaning: "influence / effect", MeaningJa: "作用・影響力", Level: N3, Category: CatAbstract},
		{ID: "n3-005", Kanji: "問題", Kana: "もんだい", Romaji: "mondai", Meaning: "problem / issue", MeaningJa: "課題・難問", Level: N3, Category: CatAbstract},
		{ID: "n3-006", Kanji: "解決", Kana: "かいけつ", Romaji: "kaiketsu", Meaning: "solution / resolution", MeaningJa: "問題を解くこと", Level: N3, Category: CatAbstract},
		{ID: "n3-007", Kanji: "関係", Kana: "かんけい", Romaji: "kankei", Meaning: "relationship / connection", MeaningJa: "つながり・関連", Level: N3, Category: CatSociety},
		{ID: "n3-008", Kanji: "社会", Kana: "しゃかい", Romaji: "shakai", Meaning: "society", MeaningJa: "世の中・社会全体", Level: N3, Category: CatSociety},
		{ID: "n3-009", Kanji: "文化", Kana: "ぶんか", Romaji: "bunka", Meaning: "culture", MeaningJa: "文明・慣習", Level: N3, Category: CatSociety},
		{ID: "n3-010", Kanji: "技術", Kana: "ぎじゅつ", Romaji: "gijutsu", Meaning: "technology / skill", MeaningJa: "テクノロジー・技巧", Level: N3, Category: CatWork},
		{ID: "n3-011", Kanji: "失敗", Kana: "しっぱい", Romaji: "shippai", Meaning: "failure / mistake", MeaningJa: "やり損ない", Level: N3, Category: CatEmotion},
		{ID: "n3-012", Kanji: "成功", Kana: "せいこう", Romaji: "seikou", Meaning: "success", MeaningJa: "うまくいくこと", Level: N3, Category: CatEmotion},
		{ID: "n3-013", Kanji: "感謝", Kana: "かんしゃ", Romaji: "kansha", Meaning: "gratitude / thanks", MeaningJa: "ありがたく思う気持ち", Level: N3, Category: CatEmotion},
		{ID: "n3-014", Kanji: "心配", Kana: "しんぱい", Romaji: "shinpai", Meaning: "worry / concern", MeaningJa: "不安・懸念", Level: N3, Category: CatEmotion},
		{ID: "n3-015", Kanji: "驚く", Kana: "おどろく", Romaji: "odoroku", Meaning: "to be surprised", MeaningJa: "びっくりする", Level: N3, Category: CatVerbs},
		{ID: "n3-016", Kanji: "集める", Kana: "あつめる", Romaji: "atsumeru", Meaning: "to collect / gather", MeaningJa: "集積する", Level: N3, Category: CatVerbs},
		{ID: "n3-017", Kanji: "比べる", Kana: "くらべる", Romaji: "kuraberu", Meaning: "to compare", MeaningJa: "対比する", Level: N3, Category: CatVerbs},
		{ID: "n3-018", Kanji: "考える", Kana: "かんがえる", Romaji: "kangaeru", Meaning: "to think / consider", MeaningJa: "思考する", Level: N3, Category: CatVerbs},
		{ID: "n3-019", Kanji: "重要", Kana: "じゅうよう", Romaji: "juuyou", Meaning: "important / significant", MeaningJa: "大切・重大", Level: N3, Category: CatAdj},
		{ID: "n3-020", Kanji: "複雑", Kana: "ふくざつ", Romaji: "fukuzatsu", Meaning: "complex / complicated", MeaningJa: "入り組んでいる", Level: N3, Category: CatAdj},

		// ── N2 ──────────────────────────────────────────────────────────
		{ID: "n2-001", Kanji: "義務", Kana: "ぎむ", Romaji: "gimu", Meaning: "duty / obligation", MeaningJa: "果たさなければならないこと", Level: N2, Category: CatSociety},
		{ID: "n2-002", Kanji: "権利", Kana: "けんり", Romaji: "kenri", Meaning: "right / entitlement", MeaningJa: "正当な資格・権限", Level: N2, Category: CatSociety},
		{ID: "n2-003", Kanji: "契約", Kana: "けいやく", Romaji: "keiyaku", Meaning: "contract / agreement", MeaningJa: "法的な合意", Level: N2, Category: CatWork},
		{ID: "n2-004", Kanji: "収入", Kana: "しゅうにゅう", Romaji: "shuunyuu", Meaning: "income / revenue", MeaningJa: "所得・稼ぎ", Level: N2, Category: CatWork},
		{ID: "n2-005", Kanji: "投資", Kana: "とうし", Romaji: "toushi", Meaning: "investment", MeaningJa: "利益を目的とした出資", Level: N2, Category: CatWork},
		{ID: "n2-006", Kanji: "環境", Kana: "かんきょう", Romaji: "kankyou", Meaning: "environment", MeaningJa: "周囲の状況・自然環境", Level: N2, Category: CatNature},
		{ID: "n2-007", Kanji: "資源", Kana: "しげん", Romaji: "shigen", Meaning: "resources", MeaningJa: "活用できる素材・エネルギー", Level: N2, Category: CatNature},
		{ID: "n2-008", Kanji: "維持", Kana: "いじ", Romaji: "iji", Meaning: "maintenance / upkeep", MeaningJa: "現状を保つこと", Level: N2, Category: CatAbstract},
		{ID: "n2-009", Kanji: "促進", Kana: "そくしん", Romaji: "sokushin", Meaning: "promotion / acceleration", MeaningJa: "物事を進めること", Level: N2, Category: CatAbstract},
		{ID: "n2-010", Kanji: "把握", Kana: "はあく", Romaji: "haaku", Meaning: "grasp / understanding", MeaningJa: "状況を正確につかむ", Level: N2, Category: CatAbstract},
		{ID: "n2-011", Kanji: "検討", Kana: "けんとう", Romaji: "kentou", Meaning: "consideration / review", MeaningJa: "詳しく調べ考えること", Level: N2, Category: CatAbstract},
		{ID: "n2-012", Kanji: "指摘", Kana: "してき", Romaji: "shiteki", Meaning: "point out / indicate", MeaningJa: "問題点などを示す", Level: N2, Category: CatVerbs},
		{ID: "n2-013", Kanji: "批判", Kana: "ひはん", Romaji: "hihan", Meaning: "criticism / critique", MeaningJa: "欠点などを論じること", Level: N2, Category: CatAbstract},
		{ID: "n2-014", Kanji: "議論", Kana: "ぎろん", Romaji: "giron", Meaning: "debate / argument", MeaningJa: "意見を出し合うこと", Level: N2, Category: CatAbstract},
		{ID: "n2-015", Kanji: "矛盾", Kana: "むじゅん", Romaji: "mujun", Meaning: "contradiction", MeaningJa: "前後が食い違うこと", Level: N2, Category: CatAbstract},

		// ── N1 ──────────────────────────────────────────────────────────
		{ID: "n1-001", Kanji: "概念", Kana: "がいねん", Romaji: "gainen", Meaning: "concept / notion", MeaningJa: "一般的な考え方・抽象的な意味", Level: N1, Category: CatAbstract},
		{ID: "n1-002", Kanji: "抽象", Kana: "ちゅうしょう", Romaji: "chuushou", Meaning: "abstraction", MeaningJa: "具体性をはぶいた考え", Level: N1, Category: CatAbstract},
		{ID: "n1-003", Kanji: "倫理", Kana: "りんり", Romaji: "rinri", Meaning: "ethics / morality", MeaningJa: "道徳的な原則", Level: N1, Category: CatSociety},
		{ID: "n1-004", Kanji: "哲学", Kana: "てつがく", Romaji: "tetsugaku", Meaning: "philosophy", MeaningJa: "存在・知識・価値を探求する学問", Level: N1, Category: CatAbstract},
		{ID: "n1-005", Kanji: "妥協", Kana: "だきょう", Romaji: "dakyou", Meaning: "compromise", MeaningJa: "互いに譲り合って解決すること", Level: N1, Category: CatAbstract},
		{ID: "n1-006", Kanji: "慣習", Kana: "かんしゅう", Romaji: "kanshuu", Meaning: "custom / convention", MeaningJa: "長く続いてきた習わし", Level: N1, Category: CatSociety},
		{ID: "n1-007", Kanji: "象徴", Kana: "しょうちょう", Romaji: "shouchou", Meaning: "symbol / emblem", MeaningJa: "何かを表すしるし", Level: N1, Category: CatAbstract},
		{ID: "n1-008", Kanji: "矜持", Kana: "きょうじ", Romaji: "kyouji", Meaning: "pride / dignity", MeaningJa: "自分を誇りに思う気持ち", Level: N1, Category: CatEmotion},
		{ID: "n1-009", Kanji: "逡巡", Kana: "しゅんじゅん", Romaji: "shunjun", Meaning: "hesitation / indecision", MeaningJa: "ためらい・決断できないこと", Level: N1, Category: CatEmotion},
		{ID: "n1-010", Kanji: "忖度", Kana: "そんたく", Romaji: "sontaku", Meaning: "reading the room / inferring wishes", MeaningJa: "相手の気持ちを推し量ること", Level: N1, Category: CatSociety, ExampleJa: "上司の意向を忖度する。", ExampleEn: "To read what the boss wants without being told."},
		{ID: "n1-011", Kanji: "斟酌", Kana: "しんしゃく", Romaji: "shinshaku", Meaning: "consideration / discretion", MeaningJa: "事情をくんで手加減すること", Level: N1, Category: CatAbstract},
		{ID: "n1-012", Kanji: "脆弱", Kana: "ぜいじゃく", Romaji: "zeijaku", Meaning: "fragile / vulnerable", MeaningJa: "もろく弱い状態", Level: N1, Category: CatAdj},
		{ID: "n1-013", Kanji: "醸成", Kana: "じょうせい", Romaji: "jousei", Meaning: "to cultivate / foster (atmosphere)", MeaningJa: "雰囲気・状況を作り上げること", Level: N1, Category: CatVerbs},
		{ID: "n1-014", Kanji: "顕著", Kana: "けんちょ", Romaji: "kencho", Meaning: "remarkable / conspicuous", MeaningJa: "はっきりと目立つ様子", Level: N1, Category: CatAdj},
		{ID: "n1-015", Kanji: "払拭", Kana: "ふっしょく", Romaji: "fusshoku", Meaning: "to dispel / wipe away", MeaningJa: "疑念・不安などを完全に取り除く", Level: N1, Category: CatVerbs},
	}

	// Assign sequential IDs for cleanliness (already set inline, but ensure uniqueness)
	seen := map[string]bool{}
	for i, w := range vocabulary {
		if seen[w.ID] {
			vocabulary[i].ID = fmt.Sprintf("%s-%03d", strings.ToLower(string(w.Level)), i)
		}
		seen[w.ID] = true
	}

	log.Printf("語彙データ読み込み完了: %d 語", len(vocabulary))
}

// ─────────────────────────────────────────────
//  Filtering helpers
// ─────────────────────────────────────────────

func filterWords(level JLPTLevel, category WordCategory) []Word {
	var result []Word
	for _, w := range vocabulary {
		if level != "" && w.Level != level {
			continue
		}
		if category != "" && w.Category != category {
			continue
		}
		result = append(result, w)
	}
	return result
}

func wordsByID(id string) (Word, bool) {
	for _, w := range vocabulary {
		if w.ID == id {
			return w, true
		}
	}
	return Word{}, false
}

// shuffle returns a shuffled copy
func shuffle(words []Word) []Word {
	cp := make([]Word, len(words))
	copy(cp, words)
	rand.Shuffle(len(cp), func(i, j int) { cp[i], cp[j] = cp[j], cp[i] })
	return cp
}

// ─────────────────────────────────────────────
//  Quiz engine
// ─────────────────────────────────────────────

func buildQuestion(sess *QuizSession) *QuizQuestion {
	if sess.CurrentIdx >= len(sess.Words) {
		return nil
	}
	word := sess.Words[sess.CurrentIdx]

	q := &QuizQuestion{
		SessionID:   sess.ID,
		QuestionNum: sess.CurrentIdx + 1,
		Total:       sess.Total,
		Mode:        sess.Mode,
		WordID:      word.ID,
		Score:       sess.Score,
		Correct:     sess.Correct,
		Wrong:       sess.Wrong,
		Streak:      sess.StreakCount,
	}

	switch sess.Mode {
	case ModeKanjiToMeaning:
		if word.Kanji != "" {
			q.Prompt = word.Kanji
			q.PromptSub = word.Kana
		} else {
			q.Prompt = word.Kana
		}
		q.Choices = buildChoices(word, "meaning", sess.Words)

	case ModeMeaningToKanji:
		q.Prompt = word.Meaning
		if word.Kanji != "" {
			q.Choices = buildChoices(word, "kanji", sess.Words)
		} else {
			q.Choices = buildChoices(word, "kana", sess.Words)
		}

	case ModeKanaToKanji:
		q.Prompt = word.Kana
		q.PromptSub = word.Romaji
		if word.Kanji != "" {
			q.Choices = buildChoices(word, "kanji", sess.Words)
		} else {
			q.Choices = buildChoices(word, "meaning", sess.Words)
		}

	case ModeFlashcard:
		if word.Kanji != "" {
			q.Prompt = word.Kanji
			q.PromptSub = word.Kana
		} else {
			q.Prompt = word.Kana
		}
		q.Answer = word.Meaning
		q.AnswerSub = word.MeaningJa
	}

	return q
}

func buildChoices(correct Word, field string, pool []Word) []Choice {
	correctChoice := Choice{
		ID:      correct.ID,
		Correct: true,
	}
	switch field {
	case "meaning":
		correctChoice.Text = correct.Meaning
		correctChoice.Sub = correct.MeaningJa
	case "kanji":
		correctChoice.Text = correct.Kanji
		correctChoice.Sub = correct.Kana
	case "kana":
		correctChoice.Text = correct.Kana
		correctChoice.Sub = correct.Romaji
	}

	// Pick 3 distractors from same level, different word
	distractors := []Choice{}
	shuffled := shuffle(pool)
	for _, w := range shuffled {
		if w.ID == correct.ID {
			continue
		}
		d := Choice{ID: w.ID, Correct: false}
		switch field {
		case "meaning":
			d.Text = w.Meaning
			d.Sub = w.MeaningJa
		case "kanji":
			if w.Kanji == "" {
				continue
			}
			d.Text = w.Kanji
			d.Sub = w.Kana
		case "kana":
			d.Text = w.Kana
			d.Sub = w.Romaji
		}
		if d.Text == "" || d.Text == correctChoice.Text {
			continue
		}
		distractors = append(distractors, d)
		if len(distractors) == 3 {
			break
		}
	}

	// Pad with cross-level distractors if needed
	if len(distractors) < 3 {
		for _, w := range vocabulary {
			if w.ID == correct.ID {
				continue
			}
			d := Choice{ID: w.ID, Correct: false}
			switch field {
			case "meaning":
				d.Text = w.Meaning
			case "kanji":
				if w.Kanji == "" {
					continue
				}
				d.Text = w.Kanji
				d.Sub = w.Kana
			case "kana":
				d.Text = w.Kana
			}
			if d.Text == "" || d.Text == correctChoice.Text {
				continue
			}
			dup := false
			for _, ex := range distractors {
				if ex.Text == d.Text {
					dup = true
					break
				}
			}
			if !dup {
				distractors = append(distractors, d)
			}
			if len(distractors) == 3 {
				break
			}
		}
	}

	choices := append(distractors, correctChoice)
	rand.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
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

// ─────────────────────────────────────────────
//  HTTP Handlers
// ─────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

// GET /api/v1/vocabulary?level=N5&category=動詞
func handleVocabulary(w http.ResponseWriter, r *http.Request) {
	level := JLPTLevel(r.URL.Query().Get("level"))
	category := WordCategory(r.URL.Query().Get("category"))

	words := filterWords(level, category)

	// Sort by level then ID
	sort.Slice(words, func(i, j int) bool {
		if words[i].Level != words[j].Level {
			return words[i].Level < words[j].Level
		}
		return words[i].ID < words[j].ID
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": len(words),
		"words": words,
		"filters": map[string]string{
			"level":    string(level),
			"category": string(category),
		},
	})
}

// GET /api/v1/vocabulary/:id
func handleVocabularyOne(w http.ResponseWriter, r *http.Request, id string) {
	word, ok := wordsByID(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "単語が見つかりません: "+id)
		return
	}
	writeJSON(w, http.StatusOK, word)
}

// GET /api/v1/vocabulary/random?level=N4&count=10
func handleRandom(w http.ResponseWriter, r *http.Request) {
	level := JLPTLevel(r.URL.Query().Get("level"))
	category := WordCategory(r.URL.Query().Get("category"))
	countStr := r.URL.Query().Get("count")
	count := 10
	if countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 && n <= 50 {
			count = n
		}
	}

	pool := filterWords(level, category)
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

// POST /api/v1/quiz/start
func handleQuizStart(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	var req struct {
		Level    JLPTLevel    `json:"level"`
		Category WordCategory `json:"category"`
		Mode     QuizMode     `json:"mode"`
		Count    int          `json:"count"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "JSONの形式が正しくありません")
		return
	}

	if req.Level == "" {
		req.Level = N5
	}
	if req.Mode == "" {
		req.Mode = ModeKanjiToMeaning
	}
	if req.Count <= 0 || req.Count > 50 {
		req.Count = 10
	}

	pool := filterWords(req.Level, req.Category)
	if len(pool) == 0 {
		writeError(w, http.StatusBadRequest, "no_words", "指定レベル・カテゴリの単語がありません")
		return
	}

	shuffled := shuffle(pool)
	if req.Count > len(shuffled) {
		req.Count = len(shuffled)
	}
	words := shuffled[:req.Count]

	sess := &QuizSession{
		ID:        fmt.Sprintf("sess-%d", time.Now().UnixNano()),
		Level:     req.Level,
		Category:  req.Category,
		Mode:      req.Mode,
		Words:     words,
		Total:     req.Count,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	store.Set(sess)

	firstQ := buildQuestion(sess)
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"session_id":     sess.ID,
		"level":          sess.Level,
		"mode":           sess.Mode,
		"total":          sess.Total,
		"first_question": firstQ,
	})
}

// POST /api/v1/quiz/answer
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

	sess, ok := store.Get(req.SessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session_not_found", "セッションが見つかりません")
		return
	}
	if sess.Finished {
		writeError(w, http.StatusConflict, "session_finished", "このセッションは既に終了しています")
		return
	}

	word, ok := wordsByID(req.WordID)
	if !ok {
		writeError(w, http.StatusNotFound, "word_not_found", "単語が見つかりません")
		return
	}

	// Determine correctness
	var isCorrect bool
	if sess.Mode == ModeFlashcard {
		if req.Correct == nil {
			writeError(w, http.StatusBadRequest, "missing_correct", "フラッシュカードモードでは correct フィールドが必要です")
			return
		}
		isCorrect = *req.Correct
	} else {
		// Multiple choice: check if chosen ID is the correct word
		isCorrect = req.ChoiceID == word.ID
	}

	// Update session
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
		sess.WrongWords = append(sess.WrongWords, word)
	}

	sess.CurrentIdx++
	sess.UpdatedAt = time.Now()

	finished := sess.CurrentIdx >= sess.Total
	sess.Finished = finished
	store.Set(sess)

	result := AnswerResult{
		Correct:         isCorrect,
		CorrectAnswer:   word.Meaning,
		CorrectSub:      word.MeaningJa,
		ExampleJa:       word.ExampleJa,
		ExampleEn:       word.ExampleEn,
		Score:           sess.Score,
		CorrectCount:    sess.Correct,
		WrongCount:      sess.Wrong,
		Streak:          sess.StreakCount,
		SessionFinished: finished,
	}

	if !finished {
		result.NextQuestion = buildQuestion(sess)
	}

	writeJSON(w, http.StatusOK, result)
}

// GET /api/v1/quiz/:id/summary
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

	duration := time.Since(sess.StartedAt).Round(time.Second).String()

	writeJSON(w, http.StatusOK, SessionSummary{
		SessionID:  sess.ID,
		Level:      sess.Level,
		Mode:       sess.Mode,
		Total:      sess.Total,
		Correct:    sess.Correct,
		Wrong:      sess.Wrong,
		Score:      sess.Score,
		Accuracy:   accuracy,
		BestStreak: sess.BestStreak,
		Duration:   duration,
		Grade:      gradeSession(sess),
		WrongWords: sess.WrongWords,
		StartedAt:  sess.StartedAt,
	})
}

// GET /api/v1/quiz/:id — current question
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
	q := buildQuestion(sess)
	writeJSON(w, http.StatusOK, q)
}

// GET /api/v1/levels
func handleLevels(w http.ResponseWriter, r *http.Request) {
	type LevelInfo struct {
		Level      JLPTLevel `json:"level"`
		WordCount  int       `json:"word_count"`
		Categories []string  `json:"categories"`
	}

	levels := []JLPTLevel{N5, N4, N3, N2, N1}
	result := []LevelInfo{}

	for _, lv := range levels {
		words := filterWords(lv, "")
		catSet := map[string]bool{}
		for _, w := range words {
			catSet[string(w.Category)] = true
		}
		cats := []string{}
		for c := range catSet {
			cats = append(cats, c)
		}
		sort.Strings(cats)

		result = append(result, LevelInfo{
			Level:      lv,
			WordCount:  len(words),
			Categories: cats,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"levels":      result,
		"total_words": len(vocabulary),
	})
}

// GET /api/v1/stats
func handleStats(w http.ResponseWriter, r *http.Request) {
	type LevelCount struct {
		Level JLPTLevel `json:"level"`
		Count int       `json:"count"`
	}

	counts := map[JLPTLevel]int{}
	for _, w := range vocabulary {
		counts[w.Level]++
	}

	lc := []LevelCount{
		{N5, counts[N5]},
		{N4, counts[N4]},
		{N3, counts[N3]},
		{N2, counts[N2]},
		{N1, counts[N1]},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_words":     len(vocabulary),
		"active_sessions": store.ActiveCount(),
		"by_level":        lc,
		"modes": []string{
			string(ModeKanjiToMeaning),
			string(ModeMeaningToKanji),
			string(ModeKanaToKanji),
			string(ModeFlashcard),
		},
	})
}

// ─────────────────────────────────────────────
//  Router
// ─────────────────────────────────────────────

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next(w, r)
		log.Printf("[%s] %s %s — %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	}
}

func router() http.Handler {
	mux := http.NewServeMux()

	// Vocabulary
	mux.HandleFunc("/api/v1/vocabulary", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		handleVocabulary(w, r)
	}))

	mux.HandleFunc("/api/v1/vocabulary/random", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		handleRandom(w, r)
	}))

	mux.HandleFunc("/api/v1/vocabulary/", loggingMiddleware(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/vocabulary/")
		if id == "" {
			handleVocabulary(w, r)
			return
		}
		handleVocabularyOne(w, r, id)
	}))

	// Quiz
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
		// /api/v1/quiz/:id or /api/v1/quiz/:id/summary
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

	// Meta
	mux.HandleFunc("/api/v1/levels", loggingMiddleware(handleLevels))
	mux.HandleFunc("/api/v1/stats", loggingMiddleware(handleStats))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "ok",
			"service": "JLPT語彙クイズAPI",
			"version": "1.0.0",
			"words":   len(vocabulary),
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, `JLPT語彙クイズAPI v1.0.0
━━━━━━━━━━━━━━━━━━━━━━

語彙エンドポイント:
  GET  /api/v1/vocabulary              全単語一覧 (?level=N5&category=動詞)
  GET  /api/v1/vocabulary/random       ランダム取得 (?level=N4&count=10)
  GET  /api/v1/vocabulary/:id          単語詳細

クイズエンドポイント:
  POST /api/v1/quiz/start              クイズ開始
  POST /api/v1/quiz/answer             回答送信
  GET  /api/v1/quiz/:id                現在の問題取得
  GET  /api/v1/quiz/:id/summary        結果サマリー

メタ:
  GET  /api/v1/levels                  レベル別情報
  GET  /api/v1/stats                   統計情報
  GET  /health                         ヘルスチェック

クイズモード:
  kanji_to_meaning  漢字 → 意味を選ぶ
  meaning_to_kanji  意味 → 漢字を選ぶ
  kana_to_kanji     かな → 漢字を選ぶ
  flashcard         フラッシュカード（自己採点）`)
	})

	return mux
}

func main() {
	rand.Seed(time.Now().UnixNano())
	initVocabulary()

	port := ":8083"
	log.Printf("JLPT語彙クイズAPI 起動中: http://localhost%s", port)
	if err := http.ListenAndServe(port, router()); err != nil {
		log.Fatal(err)
	}
}
