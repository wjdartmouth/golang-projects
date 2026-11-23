package main

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

func TestNthWeekday(t *testing.T) {
    tests := []struct {
        name string
        year, month, n int
        weekday time.Weekday
        want string
    }{
        {"coming of age 2026", 2026, 1, 2, time.Monday, "2026-01-12"},
        {"marine day 2026", 2026, 7, 3, time.Monday, "2026-07-20"},
        {"respect for aged 2026", 2026, 9, 3, time.Monday, "2026-09-21"},
        {"first sunday 2026", 2026, 2, 1, time.Sunday, "2026-02-01"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := nthWeekday(tt.year, tt.month, tt.weekday, tt.n).Format("2006-01-02")
            if got != tt.want { t.Errorf("nthWeekday() = %s, want %s", got, tt.want) }
        })
    }
}

func TestJapaneseHolidays2026(t *testing.T) {
    holidays := japaneseHolidays(2026)
    dates := make(map[string]Holiday, len(holidays))
    previous := ""
    for _, h := range holidays {
        if previous >= h.Date { t.Fatalf("holidays not strictly sorted: %s then %s", previous, h.Date) }
        dates[h.Date] = h
        previous = h.Date
    }
    tests := []struct{ date, name, kind string }{
        {"2026-01-01", "New Year's Day", "national"},
        {"2026-01-12", "Coming of Age Day", "national"},
        {"2026-05-03", "Constitution Memorial Day", "national"},
        {"2026-05-06", "Substitute Holiday", "substitute"},
        {"2026-07-20", "Marine Day", "national"},
        {"2026-09-21", "Respect for the Aged Day", "national"},
    }
    for _, tt := range tests {
        t.Run(tt.date, func(t *testing.T) {
            got, ok := dates[tt.date]
            if !ok { t.Fatalf("missing holiday %s", tt.date) }
            if got.NameEn != tt.name || got.Type != tt.kind {
                t.Errorf("holiday = (%q, %q), want (%q, %q)", got.NameEn, got.Type, tt.name, tt.kind)
            }
        })
    }
}

func TestIsBusinessDay(t *testing.T) {
    holidays := japaneseHolidays(2026)
    tests := []struct{ date string; want bool }{
        {"2026-01-01", false},
        {"2026-01-02", true},
        {"2026-01-03", false},
        {"2026-05-06", false},
        {"2026-05-07", true},
    }
    for _, tt := range tests {
        t.Run(tt.date, func(t *testing.T) {
            d, err := time.Parse("2006-01-02", tt.date)
            if err != nil { t.Fatal(err) }
            if got := isBusinessDay(d, holidays); got != tt.want {
                t.Errorf("isBusinessDay(%s) = %v, want %v", tt.date, got, tt.want)
            }
        })
    }
}

func TestHolidaysHTTP(t *testing.T) {
    tests := []struct {
        name, path string
        handler http.HandlerFunc
        status int
        contains string
    }{
        {"valid year", "/api/v1/holidays?year=2026", handleHolidays, 200, "\"year\":2026"},
        {"non numeric year", "/api/v1/holidays?year=abc", handleHolidays, 400, "invalid_year"},
        {"year out of range", "/api/v1/holidays?year=2101", handleHolidays, 400, "invalid_year"},
        {"known holiday", "/api/v1/check?date=2026-01-01", handleCheck, 200, "\"is_holiday\":true"},
        {"invalid date", "/api/v1/check?date=not-a-date", handleCheck, 400, "invalid_date"},
        {"next business day", "/api/v1/next-business-day?from=2026-01-01", handleNextBusinessDay, 200, "2026-01-02"},
        {"invalid next business day", "/api/v1/next-business-day?from=nope", handleNextBusinessDay, 400, "invalid_date"},
        {"health", "/health", handleHealth, 200, "\"status\":\"ok\""},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            recorder := httptest.NewRecorder()
            tt.handler(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
            if recorder.Code != tt.status { t.Errorf("status = %d, want %d: %s", recorder.Code, tt.status, recorder.Body.String()) }
            if !strings.Contains(recorder.Body.String(), tt.contains) {
                t.Errorf("body %q does not contain %q", recorder.Body.String(), tt.contains)
            }
            if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
                t.Errorf("Content-Type = %q, want JSON", got)
            }
        })
    }
}
