package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Holiday represents a Japanese national holiday
type Holiday struct {
	Date        string `json:"date"`
	Name        string `json:"name"`
	NameEn      string `json:"name_en"`
	Type        string `json:"type"` // "national", "substitute", "observed"
	Description string `json:"description,omitempty"`
}

// HolidayResponse wraps the API response
type HolidayResponse struct {
	Year     int       `json:"year"`
	Count    int       `json:"count"`
	Holidays []Holiday `json:"holidays"`
}

// ErrorResponse for API errors
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// japaneseHolidays returns holidays for a given year
// Based on 国民の祝日に関する法律 (Act on National Holidays)
func japaneseHolidays(year int) []Holiday {
	holidays := []Holiday{}

	// Fixed holidays
	fixed := []struct {
		month, day  int
		name, nameEn, desc string
	}{
		{1, 1, "元日", "New Year's Day", "一年の始まりを祝う"},
		{2, 11, "建国記念の日", "National Foundation Day", "建国をしのび、国を愛する心を養う"},
		{2, 23, "天皇誕生日", "Emperor's Birthday", "天皇の誕生日を祝う"},
		{4, 29, "昭和の日", "Showa Day", "激動の日々を経て、復興を遂げた昭和の時代を顧みる"},
		{5, 3, "憲法記念日", "Constitution Memorial Day", "日本国憲法の施行を記念する"},
		{5, 4, "みどりの日", "Greenery Day", "自然に親しむとともにその恩恵に感謝する"},
		{5, 5, "こどもの日", "Children's Day", "こどもの人格を重んじ、こどもの幸福をはかる"},
		{8, 11, "山の日", "Mountain Day", "山に親しむ機会を得て、山の恩恵に感謝する"},
		{11, 3, "文化の日", "Culture Day", "自由と平和を愛し、文化をすすめる"},
		{11, 23, "勤労感謝の日", "Labor Thanksgiving Day", "勤労をたっとび、生産を祝い、国民がたがいに感謝しあう"},
	}

	for _, h := range fixed {
		d := time.Date(year, time.Month(h.month), h.day, 0, 0, 0, 0, time.UTC)
		holidays = append(holidays, Holiday{
			Date:        d.Format("2006-01-02"),
			Name:        h.name,
			NameEn:      h.nameEn,
			Type:        "national",
			Description: h.desc,
		})
	}

	// Happy Monday holidays (移動祝日)
	// Coming of Age Day (成人の日) - 2nd Monday of January
	comingOfAge := nthWeekday(year, 1, time.Monday, 2)
	holidays = append(holidays, Holiday{
		Date:        comingOfAge.Format("2006-01-02"),
		Name:        "成人の日",
		NameEn:      "Coming of Age Day",
		Type:        "national",
		Description: "おとなになったことを自覚し、みずから生き抜こうとする青年を祝いはげます",
	})

	// Marine Day (海の日) - 3rd Monday of July
	marineDay := nthWeekday(year, 7, time.Monday, 3)
	holidays = append(holidays, Holiday{
		Date:        marineDay.Format("2006-01-02"),
		Name:        "海の日",
		NameEn:      "Marine Day",
		Type:        "national",
		Description: "海の恩恵に感謝するとともに、海洋国日本の繁栄を願う",
	})

	// Respect for the Aged Day (敬老の日) - 3rd Monday of September
	respectAged := nthWeekday(year, 9, time.Monday, 3)
	holidays = append(holidays, Holiday{
		Date:        respectAged.Format("2006-01-02"),
		Name:        "敬老の日",
		NameEn:      "Respect for the Aged Day",
		Type:        "national",
		Description: "多年にわたり社会につくしてきた老人を敬愛し、長寿を祝う",
	})

	// Sports Day (スポーツの日) - 2nd Monday of October
	sportsDay := nthWeekday(year, 10, time.Monday, 2)
	holidays = append(holidays, Holiday{
		Date:        sportsDay.Format("2006-01-02"),
		Name:        "スポーツの日",
		NameEn:      "Sports Day",
		Type:        "national",
		Description: "スポーツを楽しみ、他者を尊重する精神を培うとともに、健康で活力ある社会の実現を願う",
	})

	// Vernal Equinox (春分の日) - approx March 20-21
	vernalEquinox := calcVernalEquinox(year)
	holidays = append(holidays, Holiday{
		Date:        vernalEquinox.Format("2006-01-02"),
		Name:        "春分の日",
		NameEn:      "Vernal Equinox Day",
		Type:        "national",
		Description: "自然をたたえ、生物をいつくしむ",
	})

	// Autumnal Equinox (秋分の日) - approx Sept 22-23
	autumnalEquinox := calcAutumnalEquinox(year)
	holidays = append(holidays, Holiday{
		Date:        autumnalEquinox.Format("2006-01-02"),
		Name:        "秋分の日",
		NameEn:      "Autumnal Equinox Day",
		Type:        "national",
		Description: "祖先をうやまい、なくなった人々をしのぶ",
	})

	// Add substitute holidays (振替休日) - if holiday falls on Sunday, next Monday is off
	substitutes := calcSubstituteHolidays(holidays, year)
	holidays = append(holidays, substitutes...)

	// Sort by date
	sortHolidays(holidays)

	return holidays
}

func nthWeekday(year, month int, weekday time.Weekday, n int) time.Time {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	offset := int(weekday) - int(first.Weekday())
	if offset < 0 {
		offset += 7
	}
	return first.AddDate(0, 0, offset+(n-1)*7)
}

func calcVernalEquinox(year int) time.Time {
	// Approximation formula for vernal equinox
	var day int
	if year <= 1979 {
		day = int(20.8357 + 0.242194*(float64(year)-1980) - float64((year-1983)/4))
	} else if year <= 2099 {
		day = int(20.8431 + 0.242194*(float64(year)-1980) - float64((year-1980)/4))
	} else {
		day = int(21.851 + 0.242194*(float64(year)-1980) - float64((year-1980)/4))
	}
	return time.Date(year, 3, day, 0, 0, 0, 0, time.UTC)
}

func calcAutumnalEquinox(year int) time.Time {
	var day int
	if year <= 1979 {
		day = int(23.2588 + 0.242194*(float64(year)-1980) - float64((year-1983)/4))
	} else if year <= 2099 {
		day = int(23.2488 + 0.242194*(float64(year)-1980) - float64((year-1980)/4))
	} else {
		day = int(24.2488 + 0.242194*(float64(year)-1980) - float64((year-1980)/4))
	}
	return time.Date(year, 9, day, 0, 0, 0, 0, time.UTC)
}

func calcSubstituteHolidays(holidays []Holiday, year int) []Holiday {
	substitutes := []Holiday{}
	holidayDates := map[string]bool{}
	for _, h := range holidays {
		holidayDates[h.Date] = true
	}

	for _, h := range holidays {
		d, _ := time.Parse("2006-01-02", h.Date)
		if d.Weekday() == time.Sunday {
			// Find next non-holiday weekday
			next := d.AddDate(0, 0, 1)
			for holidayDates[next.Format("2006-01-02")] {
				next = next.AddDate(0, 0, 1)
			}
			if next.Year() == year {
				subDate := next.Format("2006-01-02")
				if !holidayDates[subDate] {
					substitutes = append(substitutes, Holiday{
						Date:   subDate,
						Name:   "振替休日",
						NameEn: "Substitute Holiday",
						Type:   "substitute",
						Description: fmt.Sprintf("%s の振替休日", h.Name),
					})
					holidayDates[subDate] = true
				}
			}
		}
	}
	return substitutes
}

func sortHolidays(holidays []Holiday) {
	for i := 0; i < len(holidays); i++ {
		for j := i + 1; j < len(holidays); j++ {
			if holidays[i].Date > holidays[j].Date {
				holidays[i], holidays[j] = holidays[j], holidays[i]
			}
		}
	}
}

// isBusinessDay checks if a date is a business day
func isBusinessDay(t time.Time, holidays []Holiday) bool {
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	dateStr := t.Format("2006-01-02")
	for _, h := range holidays {
		if h.Date == dateStr {
			return false
		}
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleHolidays(w http.ResponseWriter, r *http.Request) {
	yearStr := r.URL.Query().Get("year")
	year := time.Now().Year()
	if yearStr != "" {
		y, err := strconv.Atoi(yearStr)
		if err != nil || y < 1948 || y > 2100 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{
				Error:   "invalid_year",
				Message: "年は1948〜2100の範囲で指定してください",
			})
			return
		}
		year = y
	}

	holidays := japaneseHolidays(year)
	writeJSON(w, http.StatusOK, HolidayResponse{
		Year:     year,
		Count:    len(holidays),
		Holidays: holidays,
	})
}

func handleCheck(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_date",
			Message: "日付はYYYY-MM-DD形式で指定してください",
		})
		return
	}

	holidays := japaneseHolidays(t.Year())
	isHoliday := false
	var matchedHoliday *Holiday

	for i, h := range holidays {
		if h.Date == dateStr {
			isHoliday = true
			matchedHoliday = &holidays[i]
			break
		}
	}

	result := map[string]interface{}{
		"date":           dateStr,
		"weekday":        t.Weekday().String(),
		"weekday_ja":     weekdayJa(t.Weekday()),
		"is_holiday":     isHoliday,
		"is_business_day": isBusinessDay(t, holidays),
	}
	if matchedHoliday != nil {
		result["holiday"] = matchedHoliday
	}

	writeJSON(w, http.StatusOK, result)
}

func handleNextBusinessDay(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("from")
	if dateStr == "" {
		dateStr = time.Now().Format("2006-01-02")
	}

	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error:   "invalid_date",
			Message: "日付はYYYY-MM-DD形式で指定してください",
		})
		return
	}

	holidays := japaneseHolidays(t.Year())
	next := t.AddDate(0, 0, 1)
	for !isBusinessDay(next, holidays) {
		if next.Year() != t.Year() {
			holidays = append(holidays, japaneseHolidays(next.Year())...)
		}
		next = next.AddDate(0, 0, 1)
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"from":             dateStr,
		"next_business_day": next.Format("2006-01-02"),
		"weekday":          next.Weekday().String(),
		"weekday_ja":       weekdayJa(next.Weekday()),
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": "1.0.0",
		"service": "祝日API",
	})
}

func weekdayJa(w time.Weekday) string {
	names := map[time.Weekday]string{
		time.Sunday:    "日曜日",
		time.Monday:    "月曜日",
		time.Tuesday:   "火曜日",
		time.Wednesday: "水曜日",
		time.Thursday:  "木曜日",
		time.Friday:    "金曜日",
		time.Saturday:  "土曜日",
	}
	return names[w]
}

func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next(w, r)
		log.Printf("%s %s %s %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	}
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", loggingMiddleware(handleHealth))
	mux.HandleFunc("/api/v1/holidays", loggingMiddleware(handleHolidays))
	mux.HandleFunc("/api/v1/check", loggingMiddleware(handleCheck))
	mux.HandleFunc("/api/v1/next-business-day", loggingMiddleware(handleNextBusinessDay))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		endpoints := []string{
			"祝日API v1.0.0",
			"",
			"エンドポイント:",
			"  GET /api/v1/holidays?year=2025       - 年間祝日一覧",
			"  GET /api/v1/check?date=2025-01-01    - 祝日・営業日チェック",
			"  GET /api/v1/next-business-day?from=2025-12-31 - 次の営業日",
			"  GET /health                          - ヘルスチェック",
		}
		fmt.Fprintln(w, strings.Join(endpoints, "\n"))
	})

	port := ":8080"
	log.Printf("祝日API サーバー起動中: http://localhost%s", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatal(err)
	}
}
