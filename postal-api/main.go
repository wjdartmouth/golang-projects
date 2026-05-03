package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// PostalRecord holds address data for a single postal code
type PostalRecord struct {
	PostalCode     string `json:"postal_code"`     // 郵便番号 (7 digits, no hyphen)
	PostalCodeFmt  string `json:"postal_code_fmt"` // 〒XXX-XXXX
	PrefCode       string `json:"pref_code"`       // 都道府県コード
	Prefecture     string `json:"prefecture"`      // 都道府県名
	PrefectureKana string `json:"prefecture_kana"` // 都道府県カナ
	City           string `json:"city"`            // 市区町村名
	CityKana       string `json:"city_kana"`       // 市区町村カナ
	Town           string `json:"town"`            // 町域名
	TownKana       string `json:"town_kana"`       // 町域カナ
	FullAddress    string `json:"full_address"`    // 都道府県 + 市区町村 + 町域
}

// PostalDB is the in-memory database
type PostalDB struct {
	mu      sync.RWMutex
	byCode  map[string][]PostalRecord // postal code -> records
	byPref  map[string][]PostalRecord // prefecture -> records
	total   int
	loaded  bool
	loadedAt time.Time
}

var db = &PostalDB{
	byCode: make(map[string][]PostalRecord),
	byPref: make(map[string][]PostalRecord),
}

// loadFromCSV loads the Japan Post CSV data
// Format: KEN_ALL.CSV from Japan Post (日本郵便)
// https://www.post.japanpost.jp/zipcode/dl/kogaki-zip.html
func (d *PostalDB) loadFromCSV(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("CSVファイルを開けません: %w", err)
	}
	defer f.Close()

	d.mu.Lock()
	defer d.mu.Unlock()

	scanner := bufio.NewScanner(f)
	count := 0

	for scanner.Scan() {
		line := scanner.Text()
		fields := parseCSVLine(line)
		if len(fields) < 9 {
			continue
		}

		code := strings.TrimSpace(fields[2])
		if len(code) != 7 {
			continue
		}

		// Clean up town name (remove parenthetical variations)
		town := cleanTownName(fields[8])
		townKana := cleanTownName(fields[5])

		rec := PostalRecord{
			PostalCode:     code,
			PostalCodeFmt:  code[:3] + "-" + code[3:],
			PrefCode:       fields[0],
			Prefecture:     fields[6],
			PrefectureKana: fields[3],
			City:           fields[7],
			CityKana:       fields[4],
			Town:           town,
			TownKana:       townKana,
			FullAddress:    fields[6] + fields[7] + town,
		}

		d.byCode[code] = append(d.byCode[code], rec)
		d.byPref[fields[6]] = append(d.byPref[fields[6]], rec)
		count++
	}

	d.total = count
	d.loaded = true
	d.loadedAt = time.Now()
	log.Printf("郵便番号データ読み込み完了: %d 件", count)
	return scanner.Err()
}

// loadSampleData loads built-in sample data when no CSV is available
func (d *PostalDB) loadSampleData() {
	d.mu.Lock()
	defer d.mu.Unlock()

	samples := []PostalRecord{
		{"1000001", "100-0001", "13", "東京都", "トウキョウト", "千代田区", "チヨダク", "千代田", "チヨダ", "東京都千代田区千代田"},
		{"1000002", "100-0002", "13", "東京都", "トウキョウト", "千代田区", "チヨダク", "皇居外苑", "コウキョガイエン", "東京都千代田区皇居外苑"},
		{"1500001", "150-0001", "13", "東京都", "トウキョウト", "渋谷区", "シブヤク", "神宮前", "ジングウマエ", "東京都渋谷区神宮前"},
		{"1500042", "150-0042", "13", "東京都", "トウキョウト", "渋谷区", "シブヤク", "宇田川町", "ウダガワチョウ", "東京都渋谷区宇田川町"},
		{"1600022", "160-0022", "13", "東京都", "トウキョウト", "新宿区", "シンジュクク", "新宿", "シンジュク", "東京都新宿区新宿"},
		{"1060032", "106-0032", "13", "東京都", "トウキョウト", "港区", "ミナトク", "六本木", "ロッポンギ", "東京都港区六本木"},
		{"1130033", "113-0033", "13", "東京都", "トウキョウト", "文京区", "ブンキョウク", "本郷", "ホンゴウ", "東京都文京区本郷"},
		{"5300001", "530-0001", "27", "大阪府", "オオサカフ", "大阪市北区", "オオサカシキタク", "梅田", "ウメダ", "大阪府大阪市北区梅田"},
		{"5420081", "542-0081", "27", "大阪府", "オオサカフ", "大阪市中央区", "オオサカシチュウオウク", "南船場", "ミナミセンバ", "大阪府大阪市中央区南船場"},
		{"6000001", "600-0001", "26", "京都府", "キョウトフ", "京都市下京区", "キョウトシシモギョウク", "烏丸通", "カラスマドオリ", "京都府京都市下京区烏丸通"},
		{"4600008", "460-0008", "23", "愛知県", "アイチケン", "名古屋市中区", "ナゴヤシナカク", "栄", "サカエ", "愛知県名古屋市中区栄"},
		{"8100001", "810-0001", "40", "福岡県", "フクオカケン", "福岡市中央区", "フクオカシチュウオウク", "天神", "テンジン", "福岡県福岡市中央区天神"},
		{"9800811", "980-0811", "04", "宮城県", "ミヤギケン", "仙台市青葉区", "センダイシアオバク", "一番町", "イチバンチョウ", "宮城県仙台市青葉区一番町"},
		{"0600001", "060-0001", "01", "北海道", "ホッカイドウ", "札幌市中央区", "サッポロシチュウオウク", "北一条西", "キタイチジョウニシ", "北海道札幌市中央区北一条西"},
		{"2200011", "220-0011", "14", "神奈川県", "カナガワケン", "横浜市西区", "ヨコハマシニシク", "高島", "タカシマ", "神奈川県横浜市西区高島"},
	}

	for _, r := range samples {
		d.byCode[r.PostalCode] = append(d.byCode[r.PostalCode], r)
		d.byPref[r.Prefecture] = append(d.byPref[r.Prefecture], r)
	}

	d.total = len(samples)
	d.loaded = true
	d.loadedAt = time.Now()
	log.Printf("サンプルデータ読み込み完了: %d 件（本番環境では KEN_ALL.CSV を使用してください）", len(samples))
}

func cleanTownName(s string) string {
	// Remove parenthetical content like （以下に掲載がない場合）
	if idx := strings.Index(s, "（"); idx != -1 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "("); idx != -1 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "（以下に掲載がない場合）"
	}
	return s
}

func parseCSVLine(line string) []string {
	// Handle quoted CSV fields
	var fields []string
	var current strings.Builder
	inQuote := false

	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ',' && !inQuote:
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	fields = append(fields, current.String())
	return fields
}

// Lookup by postal code
func (d *PostalDB) lookup(code string) ([]PostalRecord, bool) {
	// Normalize: remove hyphen
	code = strings.ReplaceAll(code, "-", "")
	code = strings.TrimSpace(code)

	d.mu.RLock()
	defer d.mu.RUnlock()

	records, ok := d.byCode[code]
	return records, ok
}

// Search by partial address
func (d *PostalDB) search(query string, limit int) []PostalRecord {
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	var results []PostalRecord
	query = strings.TrimSpace(query)

	for _, records := range d.byCode {
		for _, r := range records {
			if strings.Contains(r.FullAddress, query) ||
				strings.Contains(r.PrefectureKana, query) ||
				strings.Contains(r.CityKana, query) {
				results = append(results, r)
				if len(results) >= limit {
					return results
				}
			}
		}
	}
	return results
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleLookup(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "missing_code",
			"message": "codeパラメータは必須です（例: ?code=1500001）",
		})
		return
	}

	// Validate: 7 digits
	normalized := strings.ReplaceAll(code, "-", "")
	if utf8.RuneCountInString(normalized) != 7 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_format",
			"message": "郵便番号は7桁で入力してください（例: 1500001 または 150-0001）",
		})
		return
	}

	records, found := db.lookup(code)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error":   "not_found",
			"message": fmt.Sprintf("郵便番号 %s は見つかりませんでした", code),
			"code":    code,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"postal_code": normalized,
		"count":       len(records),
		"addresses":   records,
	})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "missing_query",
			"message": "qパラメータは必須です（例: ?q=渋谷）",
		})
		return
	}

	results := db.search(query, 20)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":   query,
		"count":   len(results),
		"results": results,
	})
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	normalized := strings.ReplaceAll(code, "-", "")
	isValid := utf8.RuneCountInString(normalized) == 7
	exists := false

	if isValid {
		_, exists = db.lookup(code)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"postal_code": code,
		"is_valid_format": isValid,
		"exists":          exists,
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_records": db.total,
		"loaded_at":     db.loadedAt.Format(time.RFC3339),
		"data_source":   "日本郵便 KEN_ALL.CSV（サンプルデータ）",
		"note":          "本番環境では https://www.post.japanpost.jp/zipcode/ からデータをダウンロードしてください",
	})
}

func main() {
	// Try to load CSV, fall back to sample data
	csvPath := os.Getenv("POSTAL_CSV")
	if csvPath == "" {
		csvPath = "./KEN_ALL.CSV"
	}

	if err := db.loadFromCSV(csvPath); err != nil {
		log.Printf("CSVファイルが見つかりません。サンプルデータを使用します: %v", err)
		db.loadSampleData()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/postal/lookup", handleLookup)
	mux.HandleFunc("/api/v1/postal/search", handleSearch)
	mux.HandleFunc("/api/v1/postal/validate", handleValidate)
	mux.HandleFunc("/api/v1/postal/stats", handleStats)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "郵便番号API"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, `郵便番号API v1.0.0

エンドポイント:
  GET /api/v1/postal/lookup?code=1500001   - 郵便番号で住所検索
  GET /api/v1/postal/lookup?code=150-0001  - ハイフンあり対応
  GET /api/v1/postal/search?q=渋谷         - キーワード検索
  GET /api/v1/postal/validate?code=150-0001 - 郵便番号バリデーション
  GET /api/v1/postal/stats                  - データ統計
  GET /health                               - ヘルスチェック

環境変数:
  POSTAL_CSV - KEN_ALL.CSVのパス（未設定時はサンプルデータ使用）`)
	})

	log.Println("郵便番号API 起動中: http://localhost:8082")
	if err := http.ListenAndServe(":8082", mux); err != nil {
		log.Fatal(err)
	}
}
