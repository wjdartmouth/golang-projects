package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"
)

// Japan consumption tax rates
const (
	TaxRateStandard = 0.10 // 10% standard rate
	TaxRateReduced  = 0.08 // 8% reduced rate (food, newspaper)
)

// InvoiceRequest is the JSON body for creating an invoice
type InvoiceRequest struct {
	// Seller info (売り手)
	SellerName        string `json:"seller_name"`
	SellerAddress     string `json:"seller_address"`
	SellerPhone       string `json:"seller_phone"`
	SellerEmail       string `json:"seller_email"`
	SellerRegNo       string `json:"seller_registration_no"` // インボイス登録番号 T-XXXXXXXXXX
	SellerBankName    string `json:"seller_bank_name"`
	SellerBankBranch  string `json:"seller_bank_branch"`
	SellerAccountType string `json:"seller_account_type"` // 普通, 当座
	SellerAccountNo   string `json:"seller_account_no"`
	SellerAccountName string `json:"seller_account_name"`

	// Buyer info (買い手)
	BuyerName    string `json:"buyer_name"`
	BuyerAddress string `json:"buyer_address"`

	// Invoice metadata
	InvoiceNo   string `json:"invoice_no"`
	IssueDate   string `json:"issue_date"`   // YYYY-MM-DD
	DueDate     string `json:"due_date"`     // YYYY-MM-DD
	PaymentNote string `json:"payment_note"` // 振込期限等

	// Line items
	Items []InvoiceItem `json:"items"`

	// Options
	Notes string `json:"notes"`
}

// InvoiceItem is a single line item
type InvoiceItem struct {
	Description string  `json:"description"`
	Quantity    float64 `json:"quantity"`
	Unit        string  `json:"unit"`   // 個, 時間, 式, etc.
	UnitPrice   float64 `json:"unit_price"`
	TaxRate     float64 `json:"tax_rate"` // 0.10 or 0.08; defaults to 0.10
	IsReduced   bool    `json:"is_reduced"` // 軽減税率対象
}

// InvoiceResult is the computed invoice
type InvoiceResult struct {
	InvoiceNo   string `json:"invoice_no"`
	IssueDate   string `json:"issue_date"`
	DueDate     string `json:"due_date"`

	Seller InvoiceParty     `json:"seller"`
	Buyer  InvoiceParty     `json:"buyer"`
	Items  []ComputedItem   `json:"items"`

	// Tax breakdown (インボイス制度対応 - 税率別合計)
	TaxBreakdown []TaxGroup `json:"tax_breakdown"`

	SubtotalExTax  int64 `json:"subtotal_ex_tax"`  // 税抜合計
	TotalTax       int64 `json:"total_tax"`         // 消費税合計
	TotalAmount    int64 `json:"total_amount"`      // 税込合計

	PaymentNote string `json:"payment_note,omitempty"`
	BankInfo    *BankInfo `json:"bank_info,omitempty"`
	Notes       string `json:"notes,omitempty"`

	// Rendered HTML
	HTML string `json:"html"`
}

// TaxGroup groups items by tax rate (インボイス制度の要件)
type TaxGroup struct {
	TaxRate         float64 `json:"tax_rate"`
	TaxRateLabel    string  `json:"tax_rate_label"`
	SubtotalExTax   int64   `json:"subtotal_ex_tax"`
	TaxAmount       int64   `json:"tax_amount"`
	SubtotalIncTax  int64   `json:"subtotal_inc_tax"`
	IsReduced       bool    `json:"is_reduced"`
}

// ComputedItem adds calculated fields to InvoiceItem
type ComputedItem struct {
	InvoiceItem
	Amount      int64  `json:"amount"`       // 税抜金額
	TaxAmount   int64  `json:"tax_amount"`   // 消費税額
	TotalAmount int64  `json:"total_amount"` // 税込金額
	TaxLabel    string `json:"tax_label"`    // ※ for reduced rate
}

// InvoiceParty holds seller or buyer info
type InvoiceParty struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone,omitempty"`
	Email   string `json:"email,omitempty"`
	RegNo   string `json:"registration_no,omitempty"`
}

// BankInfo holds payment details
type BankInfo struct {
	BankName    string `json:"bank_name"`
	BranchName  string `json:"branch_name"`
	AccountType string `json:"account_type"`
	AccountNo   string `json:"account_no"`
	AccountName string `json:"account_name"`
}

func computeInvoice(req InvoiceRequest) InvoiceResult {
	result := InvoiceResult{
		InvoiceNo: req.InvoiceNo,
		IssueDate: req.IssueDate,
		DueDate:   req.DueDate,
		Seller: InvoiceParty{
			Name:    req.SellerName,
			Address: req.SellerAddress,
			Phone:   req.SellerPhone,
			Email:   req.SellerEmail,
			RegNo:   req.SellerRegNo,
		},
		Buyer: InvoiceParty{
			Name:    req.BuyerName,
			Address: req.BuyerAddress,
		},
		PaymentNote: req.PaymentNote,
		Notes:       req.Notes,
	}

	if req.SellerBankName != "" {
		result.BankInfo = &BankInfo{
			BankName:    req.SellerBankName,
			BranchName:  req.SellerBankBranch,
			AccountType: req.SellerAccountType,
			AccountNo:   req.SellerAccountNo,
			AccountName: req.SellerAccountName,
		}
	}

	// Tax groups map: rate -> TaxGroup
	taxGroups := map[float64]*TaxGroup{}

	for _, item := range req.Items {
		rate := item.TaxRate
		if rate == 0 {
			rate = TaxRateStandard
		}

		amount := item.Quantity * item.UnitPrice
		taxAmount := math.Floor(amount * rate) // 切り捨て (floor)
		totalAmount := amount + taxAmount

		ci := ComputedItem{
			InvoiceItem: item,
			Amount:      int64(amount),
			TaxAmount:   int64(taxAmount),
			TotalAmount: int64(totalAmount),
		}
		if item.IsReduced {
			ci.TaxLabel = "※"
		}
		result.Items = append(result.Items, ci)

		if _, ok := taxGroups[rate]; !ok {
			label := fmt.Sprintf("%.0f%%", rate*100)
			taxGroups[rate] = &TaxGroup{
				TaxRate:      rate,
				TaxRateLabel: label,
				IsReduced:    item.IsReduced,
			}
		}
		taxGroups[rate].SubtotalExTax += int64(amount)
		taxGroups[rate].TaxAmount += int64(taxAmount)
		taxGroups[rate].SubtotalIncTax += int64(totalAmount)

		result.SubtotalExTax += int64(amount)
		result.TotalTax += int64(taxAmount)
		result.TotalAmount += int64(totalAmount)
	}

	for _, g := range taxGroups {
		result.TaxBreakdown = append(result.TaxBreakdown, *g)
	}

	result.HTML = renderHTML(result, req)
	return result
}

func formatAmount(n int64) string {
	s := fmt.Sprintf("%d", n)
	result := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result += ","
		}
		result += string(c)
	}
	return "¥" + result
}

func renderHTML(inv InvoiceResult, req InvoiceRequest) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html lang="ja">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>請求書 ` + inv.InvoiceNo + `</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=Noto+Sans+JP:wght@400;500;700&display=swap');
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: 'Noto Sans JP', sans-serif; font-size: 13px; color: #1a1a2e; background: #f5f5f0; }
  .page { max-width: 794px; margin: 20px auto; background: white; padding: 48px; box-shadow: 0 4px 24px rgba(0,0,0,0.08); }
  .header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 32px; border-bottom: 3px solid #1a1a2e; padding-bottom: 24px; }
  .title { font-size: 32px; font-weight: 700; letter-spacing: 0.15em; color: #1a1a2e; }
  .invoice-meta { text-align: right; }
  .invoice-meta .invoice-no { font-size: 14px; color: #555; margin-bottom: 4px; }
  .invoice-meta .dates { font-size: 12px; color: #777; line-height: 1.8; }
  .parties { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-bottom: 32px; }
  .party-box { padding: 16px; background: #f8f8f5; border-left: 4px solid #1a1a2e; }
  .party-label { font-size: 10px; font-weight: 700; letter-spacing: 0.1em; color: #888; text-transform: uppercase; margin-bottom: 8px; }
  .party-name { font-size: 16px; font-weight: 700; margin-bottom: 4px; }
  .party-detail { font-size: 11px; color: #555; line-height: 1.8; }
  .reg-no { display: inline-block; background: #1a1a2e; color: white; padding: 2px 8px; border-radius: 2px; font-size: 10px; margin-top: 6px; letter-spacing: 0.05em; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 24px; }
  thead tr { background: #1a1a2e; color: white; }
  thead th { padding: 10px 12px; text-align: right; font-weight: 500; font-size: 11px; letter-spacing: 0.05em; }
  thead th:first-child { text-align: left; }
  tbody tr { border-bottom: 1px solid #eee; }
  tbody tr:nth-child(even) { background: #fafafa; }
  tbody td { padding: 10px 12px; text-align: right; font-size: 12px; }
  tbody td:first-child { text-align: left; }
  .reduced-mark { color: #e74c3c; font-size: 10px; margin-left: 2px; }
  .totals { margin-left: auto; width: 300px; }
  .totals table { margin-bottom: 0; }
  .totals td { padding: 6px 12px; font-size: 12px; }
  .totals .grand-total td { font-size: 16px; font-weight: 700; background: #1a1a2e; color: white; }
  .tax-note { font-size: 10px; color: #e74c3c; margin-top: 8px; }
  .bank-info { margin-top: 32px; padding: 16px; border: 1px dashed #ccc; }
  .bank-info h3 { font-size: 11px; letter-spacing: 0.1em; color: #888; margin-bottom: 8px; }
  .bank-detail { font-size: 12px; line-height: 2; }
  .notes { margin-top: 24px; padding: 12px; background: #fffde7; border-left: 3px solid #f9a825; font-size: 11px; color: #555; }
  .footer { margin-top: 32px; padding-top: 16px; border-top: 1px solid #eee; text-align: center; font-size: 10px; color: #aaa; }
  @media print { body { background: white; } .page { box-shadow: none; margin: 0; } }
</style>
</head>
<body>
<div class="page">
  <div class="header">
    <div class="title">請求書</div>
    <div class="invoice-meta">
      <div class="invoice-no">No. ` + inv.InvoiceNo + `</div>
      <div class="dates">
        発行日：` + formatDate(inv.IssueDate) + `<br>
        支払期限：` + formatDate(inv.DueDate) + `
      </div>
    </div>
  </div>

  <div class="parties">
    <div class="party-box">
      <div class="party-label">請求先 / Bill To</div>
      <div class="party-name">` + inv.Buyer.Name + ` 御中</div>
      <div class="party-detail">` + inv.Buyer.Address + `</div>
    </div>
    <div class="party-box">
      <div class="party-label">請求元 / From</div>
      <div class="party-name">` + inv.Seller.Name + `</div>
      <div class="party-detail">
        ` + inv.Seller.Address)

	if inv.Seller.Phone != "" {
		sb.WriteString(`<br>TEL: ` + inv.Seller.Phone)
	}
	if inv.Seller.Email != "" {
		sb.WriteString(`<br>` + inv.Seller.Email)
	}
	sb.WriteString(`</div>`)
	if inv.Seller.RegNo != "" {
		sb.WriteString(`<div class="reg-no">登録番号 ` + inv.Seller.RegNo + `</div>`)
	}
	sb.WriteString(`</div></div>`)

	// Items table
	sb.WriteString(`<table>
    <thead>
      <tr>
        <th style="width:40%">品目・摘要</th>
        <th>数量</th>
        <th>単位</th>
        <th>単価</th>
        <th>税率</th>
        <th>金額（税抜）</th>
      </tr>
    </thead>
    <tbody>`)

	for _, item := range inv.Items {
		reducedMark := ""
		if item.IsReduced {
			reducedMark = `<span class="reduced-mark">※</span>`
		}
		unit := item.Unit
		if unit == "" {
			unit = "式"
		}
		sb.WriteString(fmt.Sprintf(`
      <tr>
        <td>%s%s</td>
        <td>%.0f</td>
        <td>%s</td>
        <td>%s</td>
        <td>%.0f%%</td>
        <td>%s</td>
      </tr>`,
			item.Description, reducedMark,
			item.Quantity, unit,
			formatAmount(int64(item.UnitPrice)),
			item.TaxRate*100,
			formatAmount(item.Amount),
		))
	}

	sb.WriteString(`</tbody></table>`)

	// Totals
	sb.WriteString(`<div class="totals"><table>`)
	sb.WriteString(fmt.Sprintf(`
    <tr><td>小計（税抜）</td><td>%s</td></tr>`, formatAmount(inv.SubtotalExTax)))

	for _, g := range inv.TaxBreakdown {
		reducedMark := ""
		if g.IsReduced {
			reducedMark = "※"
		}
		sb.WriteString(fmt.Sprintf(`
    <tr><td>消費税 %s（%s）</td><td>%s</td></tr>`,
			g.TaxRateLabel, reducedMark, formatAmount(g.TaxAmount)))
	}

	sb.WriteString(fmt.Sprintf(`
    <tr class="grand-total"><td>合計（税込）</td><td>%s</td></tr>
  </table>`, formatAmount(inv.TotalAmount)))

	hasReduced := false
	for _, g := range inv.TaxBreakdown {
		if g.IsReduced {
			hasReduced = true
		}
	}
	if hasReduced {
		sb.WriteString(`<div class="tax-note">※ 軽減税率（8%）対象品目</div>`)
	}
	sb.WriteString(`</div>`)

	// Bank info
	if inv.BankInfo != nil {
		sb.WriteString(`<div class="bank-info">
    <h3>お振込先 / Payment Details</h3>
    <div class="bank-detail">
      ` + inv.BankInfo.BankName + ` ` + inv.BankInfo.BranchName + `支店<br>
      ` + inv.BankInfo.AccountType + ` ` + inv.BankInfo.AccountNo + `<br>
      口座名義：` + inv.BankInfo.AccountName + `
    </div>`)
		if inv.PaymentNote != "" {
			sb.WriteString(`<div style="margin-top:8px;font-size:11px;color:#888">` + inv.PaymentNote + `</div>`)
		}
		sb.WriteString(`</div>`)
	}

	if inv.Notes != "" {
		sb.WriteString(`<div class="notes">備考：` + inv.Notes + `</div>`)
	}

	sb.WriteString(`
  <div class="footer">
    Generated by 請求書API • ` + time.Now().Format("2006年01月02日") + `
  </div>
</div>
</body>
</html>`)

	return sb.String()
}

func formatDate(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%d年%02d月%02d日", t.Year(), t.Month(), t.Day())
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Return sample invoice
		sample := sampleRequest()
		result := computeInvoice(sample)
		writeJSON(w, http.StatusOK, result)
		return
	}

	var req InvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_json",
			"message": "JSONの形式が正しくありません",
		})
		return
	}

	if req.SellerName == "" || req.BuyerName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "missing_required",
			"message": "seller_name と buyer_name は必須です",
		})
		return
	}

	if req.InvoiceNo == "" {
		req.InvoiceNo = fmt.Sprintf("INV-%s-%04d",
			time.Now().Format("200601"),
			time.Now().UnixNano()%10000)
	}
	if req.IssueDate == "" {
		req.IssueDate = time.Now().Format("2006-01-02")
	}
	if req.DueDate == "" {
		req.DueDate = time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	}

	result := computeInvoice(req)

	// Return HTML if requested
	if r.URL.Query().Get("format") == "html" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(result.HTML))
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func handlePreview(w http.ResponseWriter, r *http.Request) {
	sample := sampleRequest()
	result := computeInvoice(sample)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(result.HTML))
}

func sampleRequest() InvoiceRequest {
	return InvoiceRequest{
		SellerName:        "株式会社サンプルテック",
		SellerAddress:     "〒150-0001 東京都渋谷区神宮前1-1-1",
		SellerPhone:       "03-1234-5678",
		SellerEmail:       "billing@sample-tech.co.jp",
		SellerRegNo:       "T1234567890123",
		SellerBankName:    "みずほ銀行",
		SellerBankBranch:  "渋谷",
		SellerAccountType: "普通",
		SellerAccountNo:   "1234567",
		SellerAccountName: "カ）サンプルテック",
		BuyerName:         "株式会社クライアント商事",
		BuyerAddress:      "〒100-0001 東京都千代田区丸の内1-1-1",
		InvoiceNo:         "INV-202501-0042",
		IssueDate:         "2025-01-31",
		DueDate:           "2025-02-28",
		PaymentNote:       "恐れ入りますが、上記口座へお振込みをお願いいたします。",
		Items: []InvoiceItem{
			{Description: "Webシステム開発（基本設計）", Quantity: 1, Unit: "式", UnitPrice: 500000, TaxRate: 0.10},
			{Description: "UI/UXデザイン", Quantity: 40, Unit: "時間", UnitPrice: 8000, TaxRate: 0.10},
			{Description: "サーバー保守費用（月額）", Quantity: 1, Unit: "月", UnitPrice: 30000, TaxRate: 0.10},
			{Description: "打ち合わせ用お茶・お菓子", Quantity: 3, Unit: "個", UnitPrice: 1500, TaxRate: 0.08, IsReduced: true},
		},
		Notes: "ご不明な点がございましたら、お気軽にお問い合わせください。",
	}
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/invoice/generate", handleGenerate)
	mux.HandleFunc("/api/v1/invoice/preview", handlePreview)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "請求書API"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "請求書API v1.0.0\n\nGET /api/v1/invoice/preview - サンプル請求書HTML表示\nGET /api/v1/invoice/generate - サンプルJSON\nPOST /api/v1/invoice/generate - 請求書生成 (JSON body)\nPOST /api/v1/invoice/generate?format=html - HTML形式で返す")
	})

	log.Println("請求書API 起動中: http://localhost:8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}
