// 勤怠管理API (Attendance Management API)
// 労働基準法に準拠した勤怠管理・残業計算・有給休暇管理システム
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────────────────────────
// Constants — 労働基準法 (Labor Standards Act)
// ─────────────────────────────────────────────

const (
	// 法定労働時間 Legal working hours per day/week
	LegalHoursPerDay  = 8.0  // 労基法第32条
	LegalHoursPerWeek = 40.0 // 労基法第32条

	// 残業割増賃金率 Overtime premium rates
	OvertimeRateLegal   = 1.25 // 法定外残業 (over 8h/day or 40h/week)
	OvertimeRateLate    = 1.35 // 深夜残業 22:00–05:00 (労基法第37条)
	OvertimeRateHoliday = 1.35 // 法定休日労働

	// 有給休暇 Paid leave accrual (労基法第39条)
	// Days granted based on years of continuous service
	// 0.5yr:10, 1.5yr:11, 2.5yr:12, 3.5yr:14, 4.5yr:16, 5.5yr:18, 6.5yr+:20

	// 休憩時間 Mandatory break times (労基法第34条)
	BreakMinutes45 = 45 // Required if work > 6 hours
	BreakMinutes60 = 60 // Required if work > 8 hours

	// 深夜時間帯 Late-night hours
	LateNightStart = 22 // 22:00
	LateNightEnd   = 5  // 05:00
)

// ─────────────────────────────────────────────
// Data Models
// ─────────────────────────────────────────────

// Department represents a company department
type Department struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Employee represents a company employee
type Employee struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	NameKana     string    `json:"name_kana"`
	Email        string    `json:"email"`
	Department   string    `json:"department"`
	Position     string    `json:"position"`
	HireDate     time.Time `json:"hire_date"`
	HourlyWage   float64   `json:"hourly_wage"` // 時給 (for overtime calc)
	WorkPattern  string    `json:"work_pattern"` // "standard", "flex", "shift"
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
}

// AttendanceStatus for a single day
type AttendanceStatus string

const (
	StatusPresent  AttendanceStatus = "出勤"
	StatusAbsent   AttendanceStatus = "欠勤"
	StatusLeave    AttendanceStatus = "有給休暇"
	StatusHalfLeave AttendanceStatus = "半休"
	StatusHoliday  AttendanceStatus = "休日"
	StatusRemote   AttendanceStatus = "在宅勤務"
	StatusOvertime AttendanceStatus = "残業"
)

// ClockRecord is a single clock-in or clock-out event
type ClockRecord struct {
	ID         string    `json:"id"`
	EmployeeID string    `json:"employee_id"`
	Type       string    `json:"type"` // "clock_in" | "clock_out" | "break_start" | "break_end"
	Timestamp  time.Time `json:"timestamp"`
	Location   string    `json:"location,omitempty"` // "office", "remote", "client"
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// AttendanceRecord is a computed daily attendance record
type AttendanceRecord struct {
	ID              string           `json:"id"`
	EmployeeID      string           `json:"employee_id"`
	Date            string           `json:"date"` // YYYY-MM-DD
	ClockIn         *time.Time       `json:"clock_in,omitempty"`
	ClockOut        *time.Time       `json:"clock_out,omitempty"`
	BreakMinutes    int              `json:"break_minutes"`
	WorkMinutes     int              `json:"work_minutes"`      // 実労働時間
	OvertimeMinutes int              `json:"overtime_minutes"`  // 残業時間
	LateNightMins   int              `json:"late_night_minutes"` // 深夜残業
	Status          AttendanceStatus `json:"status"`
	IsHolidayWork   bool             `json:"is_holiday_work"`
	Note            string           `json:"note,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// LeaveType 有給休暇の種類
type LeaveType string

const (
	LeaveTypeAnnual   LeaveType = "有給休暇"
	LeaveTypeHalfAM   LeaveType = "午前半休"
	LeaveTypeHalfPM   LeaveType = "午後半休"
	LeaveTypeSick     LeaveType = "病気休暇"
	LeaveTypeSpecial  LeaveType = "特別休暇"
	LeaveTypeMaternity LeaveType = "育児休暇"
)

// LeaveRequest 有給休暇申請
type LeaveRequest struct {
	ID         string    `json:"id"`
	EmployeeID string    `json:"employee_id"`
	Type       LeaveType `json:"type"`
	StartDate  string    `json:"start_date"` // YYYY-MM-DD
	EndDate    string    `json:"end_date"`   // YYYY-MM-DD
	Days       float64   `json:"days"`        // 0.5 for half-day
	Reason     string    `json:"reason,omitempty"`
	Status     string    `json:"status"` // "pending", "approved", "rejected"
	ApprovedBy string    `json:"approved_by,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// LeaveBalance 有給休暇残日数
type LeaveBalance struct {
	EmployeeID     string    `json:"employee_id"`
	FiscalYear     int       `json:"fiscal_year"`
	Granted        float64   `json:"granted"`          // 付与日数
	Used           float64   `json:"used"`             // 使用日数
	Remaining      float64   `json:"remaining"`        // 残日数
	Carried        float64   `json:"carried"`          // 繰越日数
	ExpiryDate     string    `json:"expiry_date"`      // 有効期限
	UpdatedAt      time.Time `json:"updated_at"`
}

// MonthlySummary 月次勤怠サマリー
type MonthlySummary struct {
	EmployeeID       string  `json:"employee_id"`
	EmployeeName     string  `json:"employee_name"`
	Year             int     `json:"year"`
	Month            int     `json:"month"`
	WorkDays         int     `json:"work_days"`          // 出勤日数
	ScheduledDays    int     `json:"scheduled_days"`     // 所定労働日数
	AbsentDays       int     `json:"absent_days"`        // 欠勤日数
	LeaveDays        float64 `json:"leave_days"`         // 有給使用日数
	TotalWorkMinutes int     `json:"total_work_minutes"` // 総労働時間（分）
	LegalWorkMinutes int     `json:"legal_work_minutes"` // 法定内労働
	OvertimeMinutes  int     `json:"overtime_minutes"`   // 時間外労働
	LateNightMinutes int     `json:"late_night_minutes"` // 深夜労働
	HolidayMinutes   int     `json:"holiday_minutes"`    // 休日労働
	// 給与計算
	BaseWage         float64 `json:"base_wage"`          // 基本給（概算）
	OvertimePay      float64 `json:"overtime_pay"`       // 残業代
	TotalWage        float64 `json:"total_wage"`         // 合計
	// 労務管理アラート
	Alerts           []string `json:"alerts,omitempty"`  // 過労・未取得等の警告
}

// OvertimeDetail 残業明細
type OvertimeDetail struct {
	Date            string  `json:"date"`
	WorkHours       float64 `json:"work_hours"`
	OvertimeHours   float64 `json:"overtime_hours"`
	LateNightHours  float64 `json:"late_night_hours"`
	IsHoliday       bool    `json:"is_holiday"`
	OvertimePay     float64 `json:"overtime_pay"`
}

// ─────────────────────────────────────────────
// In-Memory Store
// ─────────────────────────────────────────────

type Store struct {
	mu            sync.RWMutex
	employees     map[string]*Employee
	clockRecords  []*ClockRecord
	attendance    map[string]*AttendanceRecord // key: employeeID+date
	leaveRequests map[string]*LeaveRequest
	leaveBalances map[string]*LeaveBalance // key: employeeID+year
	departments   map[string]*Department
}

var db = &Store{
	employees:     make(map[string]*Employee),
	clockRecords:  []*ClockRecord{},
	attendance:    make(map[string]*AttendanceRecord),
	leaveRequests: make(map[string]*LeaveRequest),
	leaveBalances: make(map[string]*LeaveBalance),
	departments:   make(map[string]*Department),
}

func attendanceKey(employeeID, date string) string {
	return employeeID + ":" + date
}

func leaveBalanceKey(employeeID string, year int) string {
	return fmt.Sprintf("%s:%d", employeeID, year)
}

// ─────────────────────────────────────────────
// Seed Data
// ─────────────────────────────────────────────

func seedData() {
	jst, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Now().In(jst)

	// Departments
	depts := []Department{
		{ID: "dept-eng", Name: "エンジニアリング部"},
		{ID: "dept-sales", Name: "営業部"},
		{ID: "dept-hr", Name: "人事部"},
		{ID: "dept-finance", Name: "経理部"},
	}
	for _, d := range depts {
		dd := d
		db.departments[d.ID] = &dd
	}

	// Employees
	employees := []Employee{
		{
			ID: "emp-001", Name: "田中 太郎", NameKana: "タナカ タロウ",
			Email: "tanaka@example.co.jp", Department: "エンジニアリング部",
			Position: "シニアエンジニア", HireDate: time.Date(2020, 4, 1, 0, 0, 0, 0, jst),
			HourlyWage: 2500, WorkPattern: "flex", IsActive: true, CreatedAt: now,
		},
		{
			ID: "emp-002", Name: "鈴木 花子", NameKana: "スズキ ハナコ",
			Email: "suzuki@example.co.jp", Department: "営業部",
			Position: "営業マネージャー", HireDate: time.Date(2018, 7, 1, 0, 0, 0, 0, jst),
			HourlyWage: 3000, WorkPattern: "standard", IsActive: true, CreatedAt: now,
		},
		{
			ID: "emp-003", Name: "佐藤 健二", NameKana: "サトウ ケンジ",
			Email: "sato@example.co.jp", Department: "エンジニアリング部",
			Position: "エンジニア", HireDate: time.Date(2022, 10, 1, 0, 0, 0, 0, jst),
			HourlyWage: 2000, WorkPattern: "standard", IsActive: true, CreatedAt: now,
		},
		{
			ID: "emp-004", Name: "山田 美咲", NameKana: "ヤマダ ミサキ",
			Email: "yamada@example.co.jp", Department: "人事部",
			Position: "人事担当", HireDate: time.Date(2021, 4, 1, 0, 0, 0, 0, jst),
			HourlyWage: 2200, WorkPattern: "standard", IsActive: true, CreatedAt: now,
		},
		{
			ID: "emp-005", Name: "中村 誠", NameKana: "ナカムラ マコト",
			Email: "nakamura@example.co.jp", Department: "経理部",
			Position: "経理リーダー", HireDate: time.Date(2019, 1, 15, 0, 0, 0, 0, jst),
			HourlyWage: 2800, WorkPattern: "standard", IsActive: true, CreatedAt: now,
		},
	}
	for _, e := range employees {
		ee := e
		db.employees[e.ID] = &ee
	}

	// Seed attendance for current month
	seedAttendance(now)

	// Seed leave balances
	year := now.Year()
	balances := []LeaveBalance{
		{EmployeeID: "emp-001", FiscalYear: year, Granted: 20, Used: 3, Remaining: 17, Carried: 0, ExpiryDate: fmt.Sprintf("%d-03-31", year+1)},
		{EmployeeID: "emp-002", FiscalYear: year, Granted: 20, Used: 8, Remaining: 12, Carried: 2, ExpiryDate: fmt.Sprintf("%d-03-31", year+1)},
		{EmployeeID: "emp-003", FiscalYear: year, Granted: 11, Used: 1, Remaining: 10, Carried: 0, ExpiryDate: fmt.Sprintf("%d-03-31", year+1)},
		{EmployeeID: "emp-004", FiscalYear: year, Granted: 14, Used: 5, Remaining: 9, Carried: 0, ExpiryDate: fmt.Sprintf("%d-03-31", year+1)},
		{EmployeeID: "emp-005", FiscalYear: year, Granted: 18, Used: 2, Remaining: 16, Carried: 1, ExpiryDate: fmt.Sprintf("%d-03-31", year+1)},
	}
	for _, b := range balances {
		bb := b
		bb.UpdatedAt = now
		db.leaveBalances[leaveBalanceKey(b.EmployeeID, year)] = &bb
	}

	log.Printf("シードデータ読み込み完了: 従業員 %d 名, 勤怠記録 %d 件", len(db.employees), len(db.attendance))
}

func seedAttendance(now time.Time) {
	jst := now.Location()
	year, month := now.Year(), now.Month()
	empIDs := []string{"emp-001", "emp-002", "emp-003", "emp-004", "emp-005"}

	// Simulate work patterns for each employee this month
	patterns := map[string][]int{
		"emp-001": {9, 19},  // 標準 9–19 (flex)
		"emp-002": {8, 17},  // 早出 8–17
		"emp-003": {10, 20}, // 遅出 10–20
		"emp-004": {9, 18},  // 所定内
		"emp-005": {8, 21},  // 長時間
	}

	for day := 1; day <= now.Day(); day++ {
		date := time.Date(year, month, day, 0, 0, 0, 0, jst)
		weekday := date.Weekday()
		if weekday == time.Saturday || weekday == time.Sunday {
			continue
		}
		dateStr := date.Format("2006-01-02")

		for _, empID := range empIDs {
			pat := patterns[empID]
			// Skip some days randomly for realism (leave, absent)
			skip := (day+len(empID))%11 == 0
			if skip && day > 1 {
				// Mark as leave or absent
				status := StatusLeave
				if (day+len(empID))%3 == 0 {
					status = StatusAbsent
				}
				rec := &AttendanceRecord{
					ID:         fmt.Sprintf("att-%s-%s", empID, dateStr),
					EmployeeID: empID,
					Date:       dateStr,
					Status:     status,
					CreatedAt:  now,
					UpdatedAt:  now,
				}
				db.attendance[attendanceKey(empID, dateStr)] = rec
				continue
			}

			// Vary clock-in/out by ±30min
			inHour := pat[0]
			outHour := pat[1]
			inMin := (day * 7) % 30
			outMin := (day * 11) % 45

			clockIn := time.Date(year, month, day, inHour, inMin, 0, 0, jst)
			clockOut := time.Date(year, month, day, outHour, outMin, 0, 0, jst)

			rec := computeAttendance(empID, dateStr, &clockIn, &clockOut, 60)
			rec.ID = fmt.Sprintf("att-%s-%s", empID, dateStr)
			db.attendance[attendanceKey(empID, dateStr)] = rec
		}
	}
}

// ─────────────────────────────────────────────
// Business Logic
// ─────────────────────────────────────────────

// computeAttendance calculates work time, overtime, late-night from clock times
func computeAttendance(empID, date string, clockIn, clockOut *time.Time, breakMins int) *AttendanceRecord {
	now := time.Now()
	rec := &AttendanceRecord{
		EmployeeID:   empID,
		Date:         date,
		ClockIn:      clockIn,
		ClockOut:     clockOut,
		BreakMinutes: breakMins,
		Status:       StatusPresent,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if clockIn == nil || clockOut == nil {
		return rec
	}

	totalMins := int(clockOut.Sub(*clockIn).Minutes())
	if totalMins <= 0 {
		return rec
	}

	// Mandatory break adjustment per 労基法第34条
	if breakMins == 0 {
		if totalMins > 8*60 {
			breakMins = BreakMinutes60
		} else if totalMins > 6*60 {
			breakMins = BreakMinutes45
		}
	}
	rec.BreakMinutes = breakMins

	workMins := totalMins - breakMins
	if workMins < 0 {
		workMins = 0
	}
	rec.WorkMinutes = workMins

	// Overtime = work beyond 8h/day (法定外残業)
	legalMins := int(LegalHoursPerDay * 60)
	if workMins > legalMins {
		rec.OvertimeMinutes = workMins - legalMins
	}

	// Late-night calculation (22:00–05:00 overlap)
	rec.LateNightMins = calcLateNightMinutes(*clockIn, *clockOut)

	// Set status
	if rec.OvertimeMinutes > 0 {
		rec.Status = StatusPresent // still "present", overtime is a flag
	}

	return rec
}

// calcLateNightMinutes calculates overlap with 22:00–05:00 window
func calcLateNightMinutes(start, end time.Time) int {
	if end.Before(start) || end.Equal(start) {
		return 0
	}

	jst := start.Location()
	y, m, d := start.Date()

	lateStart := time.Date(y, m, d, LateNightStart, 0, 0, 0, jst)
	lateEnd := time.Date(y, m, d+1, LateNightEnd, 0, 0, 0, jst)

	// Also consider early morning of same day
	earlyEnd := time.Date(y, m, d, LateNightEnd, 0, 0, 0, jst)

	total := 0

	// Check overlap with 22:00–midnight portion
	s1 := maxTime(start, lateStart)
	e1 := minTime(end, time.Date(y, m, d+1, 0, 0, 0, 0, jst))
	if e1.After(s1) {
		total += int(e1.Sub(s1).Minutes())
	}

	// Check overlap with midnight–05:00 portion
	s2 := maxTime(start, time.Date(y, m, d, 0, 0, 0, 0, jst))
	e2 := minTime(end, earlyEnd)
	if e2.After(s2) {
		total += int(e2.Sub(s2).Minutes())
	}

	// Check next-day late night (midnight–05:00 next day)
	s3 := maxTime(start, time.Date(y, m, d+1, 0, 0, 0, 0, jst))
	e3 := minTime(end, lateEnd)
	if e3.After(s3) {
		total += int(e3.Sub(s3).Minutes())
	}

	return total
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// calcPaidLeaveEntitlement calculates statutory paid leave days (労基法第39条)
func calcPaidLeaveEntitlement(hireDate time.Time) float64 {
	// Months of continuous service
	now := time.Now()
	months := int(now.Sub(hireDate).Hours() / 24 / 30)

	table := []struct {
		months int
		days   float64
	}{
		{6, 10}, {18, 11}, {30, 12}, {42, 14},
		{54, 16}, {66, 18}, {78, 20},
	}

	granted := 0.0
	for _, row := range table {
		if months >= row.months {
			granted = row.days
		}
	}
	return granted
}

// calcMonthlyOvertime calculates 36協定 compliance
// Returns total monthly overtime minutes
func calcMonthlyOvertime(records []*AttendanceRecord) int {
	total := 0
	for _, r := range records {
		total += r.OvertimeMinutes
	}
	return total
}

// calcOvertimePay calculates overtime compensation
func calcOvertimePay(workMins, overtimeMins, lateNightMins int, hourlyWage float64) float64 {
	// Regular hours pay
	regularMins := workMins - overtimeMins
	regularPay := float64(regularMins) / 60 * hourlyWage

	// Overtime pay (1.25×)
	overtimePay := float64(overtimeMins) / 60 * hourlyWage * OvertimeRateLegal

	// Late-night premium (additional 0.25× on top of any rate)
	// Simplified: apply 1.35× rate to late-night hours
	lateNightPay := float64(lateNightMins) / 60 * hourlyWage * (OvertimeRateLate - 1.0)

	return math.Round(regularPay+overtimePay+lateNightPay)
}

// buildMonthlySummary aggregates attendance for a given month
func buildMonthlySummary(empID string, year, month int) (*MonthlySummary, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	emp, ok := db.employees[empID]
	if !ok {
		return nil, fmt.Errorf("従業員が見つかりません: %s", empID)
	}

	jst, _ := time.LoadLocation("Asia/Tokyo")
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, jst)
	lastDay := firstDay.AddDate(0, 1, -1)

	// Count scheduled (business) days
	scheduledDays := 0
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			scheduledDays++
		}
	}

	summary := &MonthlySummary{
		EmployeeID:    empID,
		EmployeeName:  emp.Name,
		Year:          year,
		Month:         month,
		ScheduledDays: scheduledDays,
	}

	// Aggregate attendance records
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		rec, exists := db.attendance[attendanceKey(empID, dateStr)]
		if !exists {
			continue
		}

		switch rec.Status {
		case StatusPresent, StatusRemote:
			summary.WorkDays++
			summary.TotalWorkMinutes += rec.WorkMinutes
			summary.OvertimeMinutes += rec.OvertimeMinutes
			summary.LateNightMinutes += rec.LateNightMins
			if rec.IsHolidayWork {
				summary.HolidayMinutes += rec.WorkMinutes
			}
		case StatusLeave, StatusHalfLeave:
			if rec.Status == StatusLeave {
				summary.LeaveDays++
			} else {
				summary.LeaveDays += 0.5
			}
		case StatusAbsent:
			summary.AbsentDays++
		}
	}

	// Legal work minutes (cap at 法定労働時間)
	summary.LegalWorkMinutes = summary.TotalWorkMinutes - summary.OvertimeMinutes

	// Wage calculation
	if emp.HourlyWage > 0 {
		summary.BaseWage = math.Round(float64(summary.LegalWorkMinutes) / 60 * emp.HourlyWage)
		summary.OvertimePay = math.Round(float64(summary.OvertimeMinutes)/60*emp.HourlyWage*OvertimeRateLegal +
			float64(summary.LateNightMinutes)/60*emp.HourlyWage*(OvertimeRateLate-1.0))
		summary.TotalWage = summary.BaseWage + summary.OvertimePay
	}

	// 労務管理アラート
	summary.Alerts = generateAlerts(summary)

	return summary, nil
}

// generateAlerts checks for labor law compliance issues
func generateAlerts(s *MonthlySummary) []string {
	var alerts []string

	// 過労アラート: 月45時間超の残業 (36協定の目安)
	overtimeHours := float64(s.OvertimeMinutes) / 60
	if overtimeHours > 45 {
		alerts = append(alerts, fmt.Sprintf("⚠️ 月間残業 %.1fh — 36協定の目安（45時間）を超過しています", overtimeHours))
	} else if overtimeHours > 40 {
		alerts = append(alerts, fmt.Sprintf("⚠️ 月間残業 %.1fh — 36協定上限（45時間）に近づいています", overtimeHours))
	}

	// 深夜残業アラート
	lateHours := float64(s.LateNightMinutes) / 60
	if lateHours > 20 {
		alerts = append(alerts, fmt.Sprintf("🌙 深夜労働 %.1fh — 健康管理の観点から確認が必要です", lateHours))
	}

	// 有給未取得アラート
	if s.LeaveDays == 0 && s.Month >= 3 {
		alerts = append(alerts, "📅 今月の有給休暇取得なし — 年5日取得義務（労基法第39条7項）をご確認ください")
	}

	// 欠勤アラート
	if s.AbsentDays >= 3 {
		alerts = append(alerts, fmt.Sprintf("❗ 欠勤 %d日 — 人事面談を検討してください", s.AbsentDays))
	}

	return alerts
}

// ─────────────────────────────────────────────
// HTTP Helpers
// ─────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func minsToHHMM(mins int) string {
	h := mins / 60
	m := mins % 60
	return fmt.Sprintf("%02d:%02d", h, m)
}

func parseDateParam(s string) (time.Time, error) {
	return time.Parse("2006-01-02", s)
}

// ─────────────────────────────────────────────
// Employee Handlers
// ─────────────────────────────────────────────

func handleEmployees(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listEmployees(w, r)
	case http.MethodPost:
		createEmployee(w, r)
	default:
		writeError(w, 405, "method_not_allowed", "許可されていないメソッドです")
	}
}

func listEmployees(w http.ResponseWriter, r *http.Request) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	dept := r.URL.Query().Get("department")
	var list []*Employee
	for _, e := range db.employees {
		if dept != "" && e.Department != dept {
			continue
		}
		if !e.IsActive {
			continue
		}
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })

	writeJSON(w, 200, map[string]interface{}{
		"count":     len(list),
		"employees": list,
	})
}

func createEmployee(w http.ResponseWriter, r *http.Request) {
	var emp Employee
	if err := json.NewDecoder(r.Body).Decode(&emp); err != nil {
		writeError(w, 400, "invalid_json", "JSONの形式が正しくありません")
		return
	}
	if emp.Name == "" {
		writeError(w, 400, "missing_name", "名前は必須です")
		return
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	emp.ID = fmt.Sprintf("emp-%03d", len(db.employees)+1)
	emp.IsActive = true
	emp.CreatedAt = time.Now()
	if emp.HireDate.IsZero() {
		emp.HireDate = time.Now()
	}
	db.employees[emp.ID] = &emp

	writeJSON(w, 201, emp)
}

func handleEmployee(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		db.mu.RLock()
		emp, ok := db.employees[id]
		db.mu.RUnlock()
		if !ok {
			writeError(w, 404, "not_found", "従業員が見つかりません")
			return
		}
		writeJSON(w, 200, emp)

	case http.MethodPut, http.MethodPatch:
		db.mu.Lock()
		defer db.mu.Unlock()
		emp, ok := db.employees[id]
		if !ok {
			writeError(w, 404, "not_found", "従業員が見つかりません")
			return
		}
		var update Employee
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			writeError(w, 400, "invalid_json", "JSONの形式が正しくありません")
			return
		}
		if update.Name != "" {
			emp.Name = update.Name
		}
		if update.Department != "" {
			emp.Department = update.Department
		}
		if update.Position != "" {
			emp.Position = update.Position
		}
		if update.HourlyWage > 0 {
			emp.HourlyWage = update.HourlyWage
		}
		writeJSON(w, 200, emp)

	default:
		writeError(w, 405, "method_not_allowed", "許可されていないメソッドです")
	}
}

// ─────────────────────────────────────────────
// Clock In/Out Handlers
// ─────────────────────────────────────────────

func handleClock(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "POSTのみ対応")
		return
	}

	var req struct {
		EmployeeID string `json:"employee_id"`
		Type       string `json:"type"` // clock_in, clock_out, break_start, break_end
		Timestamp  string `json:"timestamp,omitempty"` // optional, defaults to now
		Location   string `json:"location,omitempty"`
		Note       string `json:"note,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid_json", "JSONの形式が正しくありません")
		return
	}

	validTypes := map[string]bool{
		"clock_in": true, "clock_out": true,
		"break_start": true, "break_end": true,
	}
	if !validTypes[req.Type] {
		writeError(w, 400, "invalid_type", "typeは clock_in/clock_out/break_start/break_end のいずれかです")
		return
	}

	db.mu.RLock()
	_, ok := db.employees[req.EmployeeID]
	db.mu.RUnlock()
	if !ok {
		writeError(w, 404, "employee_not_found", "従業員が見つかりません")
		return
	}

	jst, _ := time.LoadLocation("Asia/Tokyo")
	ts := time.Now().In(jst)
	if req.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339, req.Timestamp); err == nil {
			ts = t.In(jst)
		}
	}

	record := &ClockRecord{
		ID:         fmt.Sprintf("clk-%d", time.Now().UnixNano()),
		EmployeeID: req.EmployeeID,
		Type:       req.Type,
		Timestamp:  ts,
		Location:   req.Location,
		Note:       req.Note,
		CreatedAt:  time.Now(),
	}

	db.mu.Lock()
	db.clockRecords = append(db.clockRecords, record)

	// Update attendance record for today
	dateStr := ts.Format("2006-01-02")
	key := attendanceKey(req.EmployeeID, dateStr)
	att, exists := db.attendance[key]
	if !exists {
		att = &AttendanceRecord{
			ID:         fmt.Sprintf("att-%s-%s", req.EmployeeID, dateStr),
			EmployeeID: req.EmployeeID,
			Date:       dateStr,
			Status:     StatusPresent,
			CreatedAt:  time.Now(),
		}
		db.attendance[key] = att
	}

	if req.Type == "clock_in" {
		att.ClockIn = &ts
		att.Status = StatusPresent
	} else if req.Type == "clock_out" {
		att.ClockOut = &ts
		if att.ClockIn != nil {
			updated := computeAttendance(req.EmployeeID, dateStr, att.ClockIn, att.ClockOut, att.BreakMinutes)
			att.WorkMinutes = updated.WorkMinutes
			att.OvertimeMinutes = updated.OvertimeMinutes
			att.LateNightMins = updated.LateNightMins
			att.BreakMinutes = updated.BreakMinutes
		}
	}
	att.UpdatedAt = time.Now()
	db.mu.Unlock()

	typeLabel := map[string]string{
		"clock_in": "出勤", "clock_out": "退勤",
		"break_start": "休憩開始", "break_end": "休憩終了",
	}

	writeJSON(w, 201, map[string]interface{}{
		"record":    record,
		"type_ja":   typeLabel[req.Type],
		"timestamp": ts.Format("2006年01月02日 15:04:05"),
		"message":   fmt.Sprintf("%s を記録しました", typeLabel[req.Type]),
	})
}

// ─────────────────────────────────────────────
// Attendance Handlers
// ─────────────────────────────────────────────

func handleAttendance(w http.ResponseWriter, r *http.Request) {
	empID := r.URL.Query().Get("employee_id")
	dateStr := r.URL.Query().Get("date")
	monthStr := r.URL.Query().Get("month") // YYYY-MM

	if empID == "" {
		writeError(w, 400, "missing_employee_id", "employee_id は必須です")
		return
	}

	db.mu.RLock()
	_, ok := db.employees[empID]
	db.mu.RUnlock()
	if !ok {
		writeError(w, 404, "employee_not_found", "従業員が見つかりません")
		return
	}

	if dateStr != "" {
		// Single day
		db.mu.RLock()
		rec, exists := db.attendance[attendanceKey(empID, dateStr)]
		db.mu.RUnlock()
		if !exists {
			writeJSON(w, 200, map[string]interface{}{
				"employee_id": empID,
				"date":        dateStr,
				"status":      "記録なし",
			})
			return
		}
		writeJSON(w, 200, rec)
		return
	}

	// Monthly attendance list
	if monthStr == "" {
		jst, _ := time.LoadLocation("Asia/Tokyo")
		monthStr = time.Now().In(jst).Format("2006-01")
	}

	parts := strings.Split(monthStr, "-")
	if len(parts) != 2 {
		writeError(w, 400, "invalid_month", "monthはYYYY-MM形式で指定してください")
		return
	}
	year := 0
	month := 0
	fmt.Sscanf(parts[0], "%d", &year)
	fmt.Sscanf(parts[1], "%d", &month)

	jst, _ := time.LoadLocation("Asia/Tokyo")
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, jst)
	lastDay := firstDay.AddDate(0, 1, -1)

	db.mu.RLock()
	var records []*AttendanceRecord
	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		if rec, ok := db.attendance[attendanceKey(empID, dateStr)]; ok {
			records = append(records, rec)
		}
	}
	db.mu.RUnlock()

	totalWork := 0
	totalOT := 0
	for _, r := range records {
		totalWork += r.WorkMinutes
		totalOT += r.OvertimeMinutes
	}

	writeJSON(w, 200, map[string]interface{}{
		"employee_id":         empID,
		"month":               monthStr,
		"records":             records,
		"count":               len(records),
		"total_work_time":     minsToHHMM(totalWork),
		"total_overtime":      minsToHHMM(totalOT),
		"total_work_minutes":  totalWork,
		"total_overtime_minutes": totalOT,
	})
}

func handleAttendanceUpdate(w http.ResponseWriter, r *http.Request, empID, dateStr string) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		writeError(w, 405, "method_not_allowed", "PUT/PATCHのみ対応")
		return
	}

	var req struct {
		ClockIn      string `json:"clock_in"`
		ClockOut     string `json:"clock_out"`
		BreakMinutes int    `json:"break_minutes"`
		Status       string `json:"status"`
		Note         string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid_json", "JSONの形式が正しくありません")
		return
	}

	jst, _ := time.LoadLocation("Asia/Tokyo")
	db.mu.Lock()
	defer db.mu.Unlock()

	key := attendanceKey(empID, dateStr)
	att, exists := db.attendance[key]
	if !exists {
		att = &AttendanceRecord{
			ID:         fmt.Sprintf("att-%s-%s", empID, dateStr),
			EmployeeID: empID,
			Date:       dateStr,
			CreatedAt:  time.Now(),
		}
		db.attendance[key] = att
	}

	if req.ClockIn != "" {
		if t, err := time.Parse("15:04", req.ClockIn); err == nil {
			d, _ := time.Parse("2006-01-02", dateStr)
			ts := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, jst)
			att.ClockIn = &ts
		}
	}
	if req.ClockOut != "" {
		if t, err := time.Parse("15:04", req.ClockOut); err == nil {
			d, _ := time.Parse("2006-01-02", dateStr)
			ts := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, jst)
			att.ClockOut = &ts
		}
	}
	if req.BreakMinutes > 0 {
		att.BreakMinutes = req.BreakMinutes
	}
	if req.Status != "" {
		att.Status = AttendanceStatus(req.Status)
	}
	if req.Note != "" {
		att.Note = req.Note
	}

	if att.ClockIn != nil && att.ClockOut != nil {
		updated := computeAttendance(empID, dateStr, att.ClockIn, att.ClockOut, att.BreakMinutes)
		att.WorkMinutes = updated.WorkMinutes
		att.OvertimeMinutes = updated.OvertimeMinutes
		att.LateNightMins = updated.LateNightMins
	}
	att.UpdatedAt = time.Now()

	writeJSON(w, 200, att)
}

// ─────────────────────────────────────────────
// Leave / 有給休暇 Handlers
// ─────────────────────────────────────────────

func handleLeave(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listLeaveRequests(w, r)
	case http.MethodPost:
		createLeaveRequest(w, r)
	default:
		writeError(w, 405, "method_not_allowed", "許可されていないメソッドです")
	}
}

func listLeaveRequests(w http.ResponseWriter, r *http.Request) {
	empID := r.URL.Query().Get("employee_id")
	status := r.URL.Query().Get("status")

	db.mu.RLock()
	defer db.mu.RUnlock()

	var list []*LeaveRequest
	for _, req := range db.leaveRequests {
		if empID != "" && req.EmployeeID != empID {
			continue
		}
		if status != "" && req.Status != status {
			continue
		}
		list = append(list, req)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].StartDate > list[j].StartDate })

	writeJSON(w, 200, map[string]interface{}{"count": len(list), "requests": list})
}

func createLeaveRequest(w http.ResponseWriter, r *http.Request) {
	var req LeaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid_json", "JSONの形式が正しくありません")
		return
	}
	if req.EmployeeID == "" || req.StartDate == "" {
		writeError(w, 400, "missing_fields", "employee_id と start_date は必須です")
		return
	}

	db.mu.RLock()
	_, ok := db.employees[req.EmployeeID]
	db.mu.RUnlock()
	if !ok {
		writeError(w, 404, "employee_not_found", "従業員が見つかりません")
		return
	}

	if req.EndDate == "" {
		req.EndDate = req.StartDate
	}
	if req.Days <= 0 {
		req.Days = 1
	}
	if string(req.Type) == "" {
		req.Type = LeaveTypeAnnual
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	req.ID = fmt.Sprintf("leave-%d", time.Now().UnixNano())
	req.Status = "pending"
	req.CreatedAt = time.Now()
	req.UpdatedAt = time.Now()
	db.leaveRequests[req.ID] = &req

	writeJSON(w, 201, req)
}

func handleLeaveApprove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "POSTのみ対応")
		return
	}

	var body struct {
		Action     string `json:"action"` // "approve" or "reject"
		ApprovedBy string `json:"approved_by"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	db.mu.Lock()
	defer db.mu.Unlock()

	req, ok := db.leaveRequests[id]
	if !ok {
		writeError(w, 404, "not_found", "申請が見つかりません")
		return
	}

	now := time.Now()
	if body.Action == "approve" {
		req.Status = "approved"
		req.ApprovedBy = body.ApprovedBy
		req.ApprovedAt = &now

		// Update attendance records for each leave day
		jst, _ := time.LoadLocation("Asia/Tokyo")
		start, _ := parseDateParam(req.StartDate)
		end, _ := parseDateParam(req.EndDate)
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			dateStr := d.Format("2006-01-02")
			key := attendanceKey(req.EmployeeID, dateStr)
			status := StatusLeave
			if req.Type == LeaveTypeHalfAM || req.Type == LeaveTypeHalfPM {
				status = StatusHalfLeave
			}
			db.attendance[key] = &AttendanceRecord{
				ID:         fmt.Sprintf("att-%s-%s", req.EmployeeID, dateStr),
				EmployeeID: req.EmployeeID,
				Date:       dateStr,
				Status:     status,
				Note:       fmt.Sprintf("有給休暇承認 (申請ID: %s)", req.ID),
				CreatedAt:  time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, jst),
				UpdatedAt:  now,
			}
		}

		// Update leave balance
		year := time.Now().Year()
		key := leaveBalanceKey(req.EmployeeID, year)
		if bal, ok := db.leaveBalances[key]; ok {
			bal.Used += req.Days
			bal.Remaining -= req.Days
			if bal.Remaining < 0 {
				bal.Remaining = 0
			}
			bal.UpdatedAt = now
		}
	} else {
		req.Status = "rejected"
	}
	req.UpdatedAt = now

	writeJSON(w, 200, req)
}

func handleLeaveBalance(w http.ResponseWriter, r *http.Request) {
	empID := r.URL.Query().Get("employee_id")
	if empID == "" {
		// Return all balances
		db.mu.RLock()
		defer db.mu.RUnlock()
		var list []*LeaveBalance
		for _, b := range db.leaveBalances {
			list = append(list, b)
		}
		writeJSON(w, 200, map[string]interface{}{"count": len(list), "balances": list})
		return
	}

	db.mu.RLock()
	emp, ok := db.employees[empID]
	db.mu.RUnlock()
	if !ok {
		writeError(w, 404, "employee_not_found", "従業員が見つかりません")
		return
	}

	year := time.Now().Year()
	db.mu.RLock()
	bal, exists := db.leaveBalances[leaveBalanceKey(empID, year)]
	db.mu.RUnlock()

	if !exists {
		// Calculate entitlement from hire date
		granted := calcPaidLeaveEntitlement(emp.HireDate)
		writeJSON(w, 200, map[string]interface{}{
			"employee_id":  empID,
			"fiscal_year":  year,
			"granted":      granted,
			"used":         0,
			"remaining":    granted,
			"entitlement":  fmt.Sprintf("勤続%.1f年 → 年次有給付与 %.0f日", time.Since(emp.HireDate).Hours()/24/365, granted),
		})
		return
	}

	writeJSON(w, 200, bal)
}

// ─────────────────────────────────────────────
// Summary / Report Handlers
// ─────────────────────────────────────────────

func handleMonthlySummary(w http.ResponseWriter, r *http.Request) {
	empID := r.URL.Query().Get("employee_id")
	monthStr := r.URL.Query().Get("month")

	jst, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Now().In(jst)
	year, month := now.Year(), int(now.Month())

	if monthStr != "" {
		parts := strings.Split(monthStr, "-")
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &year)
			fmt.Sscanf(parts[1], "%d", &month)
		}
	}

	if empID != "" {
		// Single employee
		summary, err := buildMonthlySummary(empID, year, month)
		if err != nil {
			writeError(w, 404, "not_found", err.Error())
			return
		}
		writeJSON(w, 200, summary)
		return
	}

	// All employees
	db.mu.RLock()
	empIDs := make([]string, 0, len(db.employees))
	for id := range db.employees {
		empIDs = append(empIDs, id)
	}
	db.mu.RUnlock()
	sort.Strings(empIDs)

	var summaries []*MonthlySummary
	for _, id := range empIDs {
		s, err := buildMonthlySummary(id, year, month)
		if err == nil {
			summaries = append(summaries, s)
		}
	}

	// Department totals
	totalOT := 0
	totalAlerts := 0
	for _, s := range summaries {
		totalOT += s.OvertimeMinutes
		totalAlerts += len(s.Alerts)
	}

	writeJSON(w, 200, map[string]interface{}{
		"year":                  year,
		"month":                 month,
		"employee_count":        len(summaries),
		"total_overtime_minutes": totalOT,
		"total_overtime":        minsToHHMM(totalOT),
		"alert_count":           totalAlerts,
		"summaries":             summaries,
	})
}

func handleOvertimeReport(w http.ResponseWriter, r *http.Request) {
	empID := r.URL.Query().Get("employee_id")
	monthStr := r.URL.Query().Get("month")

	if empID == "" {
		writeError(w, 400, "missing_employee_id", "employee_id は必須です")
		return
	}

	jst, _ := time.LoadLocation("Asia/Tokyo")
	now := time.Now().In(jst)
	year, month := now.Year(), int(now.Month())

	if monthStr != "" {
		parts := strings.Split(monthStr, "-")
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &year)
			fmt.Sscanf(parts[1], "%d", &month)
		}
	}

	db.mu.RLock()
	emp, ok := db.employees[empID]
	if !ok {
		db.mu.RUnlock()
		writeError(w, 404, "not_found", "従業員が見つかりません")
		return
	}

	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, jst)
	lastDay := firstDay.AddDate(0, 1, -1)

	var details []OvertimeDetail
	totalOT := 0
	totalLN := 0
	totalPay := 0.0

	for d := firstDay; !d.After(lastDay); d = d.AddDate(0, 0, 1) {
		dateStr := d.Format("2006-01-02")
		rec, exists := db.attendance[attendanceKey(empID, dateStr)]
		if !exists || rec.OvertimeMinutes == 0 {
			continue
		}
		pay := calcOvertimePay(rec.WorkMinutes, rec.OvertimeMinutes, rec.LateNightMins, emp.HourlyWage)
		details = append(details, OvertimeDetail{
			Date:           dateStr,
			WorkHours:      math.Round(float64(rec.WorkMinutes)/60*10) / 10,
			OvertimeHours:  math.Round(float64(rec.OvertimeMinutes)/60*10) / 10,
			LateNightHours: math.Round(float64(rec.LateNightMins)/60*10) / 10,
			IsHoliday:      rec.IsHolidayWork,
			OvertimePay:    pay,
		})
		totalOT += rec.OvertimeMinutes
		totalLN += rec.LateNightMins
		totalPay += pay
	}
	db.mu.RUnlock()

	writeJSON(w, 200, map[string]interface{}{
		"employee_id":     empID,
		"employee_name":   emp.Name,
		"year":            year,
		"month":           month,
		"details":         details,
		"total_overtime":  minsToHHMM(totalOT),
		"total_late_night": minsToHHMM(totalLN),
		"total_pay":       totalPay,
		"overtime_rate":   OvertimeRateLegal,
		"late_night_rate": OvertimeRateLate,
		"hourly_wage":     emp.HourlyWage,
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	totalEmp := len(db.employees)
	totalAtt := len(db.attendance)
	totalLeave := len(db.leaveRequests)

	// Today's attendance
	jst, _ := time.LoadLocation("Asia/Tokyo")
	today := time.Now().In(jst).Format("2006-01-02")
	todayPresent := 0
	todayLeave := 0
	for _, rec := range db.attendance {
		if rec.Date == today {
			if rec.Status == StatusPresent || rec.Status == StatusRemote {
				todayPresent++
			} else if rec.Status == StatusLeave {
				todayLeave++
			}
		}
	}

	writeJSON(w, 200, map[string]interface{}{
		"total_employees":     totalEmp,
		"total_attendance":    totalAtt,
		"total_leave_requests": totalLeave,
		"today": map[string]interface{}{
			"date":          today,
			"present":       todayPresent,
			"on_leave":      todayLeave,
			"absent":        totalEmp - todayPresent - todayLeave,
		},
		"overtime_rate":    OvertimeRateLegal,
		"late_night_rate":  OvertimeRateLate,
		"legal_hours_day":  LegalHoursPerDay,
		"legal_hours_week": LegalHoursPerWeek,
	})
}

// ─────────────────────────────────────────────
// Router
// ─────────────────────────────────────────────

func logging(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(204)
			return
		}
		next(w, r)
		log.Printf("[%s] %s — %v", r.Method, r.URL.Path, time.Since(start))
	}
}

func main() {
	seedData()

	mux := http.NewServeMux()

	// Employees
	mux.HandleFunc("/api/v1/employees", logging(handleEmployees))
	mux.HandleFunc("/api/v1/employees/", logging(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/employees/")
		if id == "" {
			handleEmployees(w, r)
			return
		}
		handleEmployee(w, r, id)
	}))

	// Clock in/out
	mux.HandleFunc("/api/v1/clock", logging(handleClock))

	// Attendance
	mux.HandleFunc("/api/v1/attendance", logging(handleAttendance))
	mux.HandleFunc("/api/v1/attendance/", logging(func(w http.ResponseWriter, r *http.Request) {
		// /api/v1/attendance/:empID/:date
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/attendance/")
		parts := strings.Split(path, "/")
		if len(parts) == 2 {
			handleAttendanceUpdate(w, r, parts[0], parts[1])
			return
		}
		handleAttendance(w, r)
	}))

	// Leave / 有給
	mux.HandleFunc("/api/v1/leave", logging(handleLeave))
	mux.HandleFunc("/api/v1/leave/balance", logging(handleLeaveBalance))
	mux.HandleFunc("/api/v1/leave/", logging(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/leave/")
		if strings.HasSuffix(path, "/approve") {
			id := strings.TrimSuffix(path, "/approve")
			handleLeaveApprove(w, r, id)
			return
		}
		handleLeave(w, r)
	}))

	// Reports
	mux.HandleFunc("/api/v1/reports/monthly", logging(handleMonthlySummary))
	mux.HandleFunc("/api/v1/reports/overtime", logging(handleOvertimeReport))

	// Stats
	mux.HandleFunc("/api/v1/stats", logging(handleStats))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{
			"status":  "ok",
			"service": "勤怠管理API",
			"version": "1.0.0",
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, `勤怠管理API v1.0.0
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

従業員管理:
  GET  /api/v1/employees                        従業員一覧
  POST /api/v1/employees                        従業員登録
  GET  /api/v1/employees/:id                    従業員詳細
  PUT  /api/v1/employees/:id                    従業員更新

打刻:
  POST /api/v1/clock                            出勤・退勤・休憩打刻

勤怠:
  GET  /api/v1/attendance?employee_id=&month=   月次勤怠一覧
  GET  /api/v1/attendance?employee_id=&date=    日次勤怠
  PUT  /api/v1/attendance/:empID/:date          勤怠修正

有給休暇:
  GET  /api/v1/leave?employee_id=               有給申請一覧
  POST /api/v1/leave                            有給申請
  POST /api/v1/leave/:id/approve               承認・却下
  GET  /api/v1/leave/balance?employee_id=       有給残高

レポート:
  GET  /api/v1/reports/monthly?employee_id=&month=  月次サマリー
  GET  /api/v1/reports/overtime?employee_id=&month= 残業明細

  GET  /api/v1/stats                            統計情報`)
	})

	port := ":8084"
	log.Printf("勤怠管理API 起動中: http://localhost%s", port)
	log.Fatal(http.ListenAndServe(port, mux))
}
