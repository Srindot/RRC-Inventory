package main

// Tests for the public endpoints' guard rails: what an upload may be, how big
// a request may get, what the public lists reveal, who may return an item,
// what a borrow form must contain, and how often someone may guess a password.

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// fileHeader builds a multipart file header the way gin hands one to a
// handler, so validateImageFile reads real bytes.
func fileHeader(t *testing.T, name string, content []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("item_photo", name)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(content)
	w.Close()

	form, err := multipart.NewReader(&body, w.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	return form.File["item_photo"][0]
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidateImageFile(t *testing.T) {
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}, make([]byte, 64)...)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	html := []byte(`<html><script>alert(document.cookie)</script></html>`)

	cases := []struct {
		name    string
		file    string
		content []byte
		wantExt string
		wantErr bool
	}{
		{name: "real png", file: "photo.png", content: pngBytes(t), wantExt: ".png"},
		{name: "real jpeg", file: "IMG_0001.JPG", content: jpeg, wantExt: ".jpg"},
		// The stored extension follows the content, not the name
		{name: "png named jpg", file: "photo.jpg", content: pngBytes(t), wantExt: ".png"},
		{name: "svg is refused", file: "logo.svg", content: svg, wantErr: true},
		{name: "svg renamed to png is refused", file: "logo.png", content: svg, wantErr: true},
		{name: "html posing as jpeg is refused", file: "photo.jpg", content: html, wantErr: true},
		{name: "plain text is refused", file: "photo.webp", content: []byte("hello"), wantErr: true},
		{name: "empty file is refused", file: "photo.png", content: nil, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, err := validateImageFile(fileHeader(t, tc.file, tc.content))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("accepted %q, want refusal", tc.file)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if ext != tc.wantExt {
				t.Errorf("ext = %q, want %q", ext, tc.wantExt)
			}
		})
	}
}

func TestLimitBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const limit = 1024

	router := gin.New()
	router.POST("/upload", limitBody(limit), func(c *gin.Context) {
		if _, err := c.MultipartForm(); err != nil {
			if bodyTooLarge(err) {
				c.JSON(413, gin.H{"error": "too large"})
				return
			}
			c.JSON(400, gin.H{"error": "bad form"})
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})

	form := func(size int) (*bytes.Buffer, string) {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		part, _ := w.CreateFormFile("file", "x.bin")
		part.Write(bytes.Repeat([]byte("a"), size))
		w.Close()
		return &body, w.FormDataContentType()
	}

	cases := []struct {
		name       string
		size       int
		hideLength bool // a chunked request has no Content-Length to check up front
		wantStatus int
	}{
		{name: "small upload passes", size: 100, wantStatus: 200},
		{name: "declared oversized body is refused", size: 4 * limit, wantStatus: 413},
		{name: "undeclared oversized body is cut off", size: 4 * limit, hideLength: true, wantStatus: 413},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := form(tc.size)
			req := httptest.NewRequest("POST", "/upload", body)
			req.Header.Set("Content-Type", contentType)
			if tc.hideLength {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestPrinterBodyLimitFitsMaxUpload(t *testing.T) {
	if printerBodyLimit <= maxUploadBytes {
		t.Fatalf("printer body limit %d leaves no room for a %d byte file", printerBodyLimit, maxUploadBytes)
	}
}

func TestPhotoHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/photo", photoHeaders, func(c *gin.Context) { c.String(200, "x") })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/photo", nil))

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("Content-Security-Policy = %q", csp)
	}
}

func TestPublicListsHidePhones(t *testing.T) {
	loans := []Loan{{
		BorrowerName:  "Asha",
		BorrowerPhone: "9876543210",
		ItemName:      "Oscilloscope",
		Purpose:       "private project notes",
		Status:        "active",
	}}
	bookings := []Booking{{BookedBy: "Ravi", Phone: "9123456780", Purpose: "Gait capture"}}

	cases := []struct {
		name      string
		value     interface{}
		forbidden []string
		required  []string
	}{
		{
			name:      "loans",
			value:     toPublicLoans(loans),
			forbidden: []string{"9876543210", "borrower_phone", "private project notes"},
			required:  []string{`"borrower_name":"Asha"`, `"item_name":"Oscilloscope"`, `"status":"active"`, `"ID"`},
		},
		{
			name:      "bookings",
			value:     toPublicBookings(bookings),
			forbidden: []string{"9123456780", `"phone"`},
			required:  []string{`"booked_by":"Ravi"`, `"start_time"`, `"ID"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range tc.forbidden {
				if strings.Contains(string(raw), s) {
					t.Errorf("public JSON contains %q: %s", s, raw)
				}
			}
			for _, s := range tc.required {
				if !strings.Contains(string(raw), s) {
					t.Errorf("public JSON is missing %q: %s", s, raw)
				}
			}
		})
	}
}

func TestCheckReturn(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		stored  string
		given   string
		wantErr error
	}{
		{name: "active with matching phone", status: "active", stored: "9876543210", given: "9876543210"},
		{name: "spacing and country code are ignored", status: "active", stored: "9876543210", given: "+91 98765 43210"},
		{name: "wrong phone", status: "active", stored: "9876543210", given: "9876543211", wantErr: errReturnPhoneMismatch},
		{name: "empty phone", status: "active", stored: "9876543210", given: "", wantErr: errReturnPhoneMismatch},
		{name: "empty stored phone matches nothing", status: "active", stored: "", given: "", wantErr: errReturnPhoneMismatch},
		{name: "already returned", status: "returned", stored: "9876543210", given: "9876543210", wantErr: errReturnAlreadyDone},
		{name: "missing items go through an admin", status: "not_found", stored: "9876543210", given: "9876543210", wantErr: errReturnNotActive},
		{name: "unknown status", status: "", stored: "9876543210", given: "9876543210", wantErr: errReturnNotActive},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkReturn(Loan{Status: tc.status, BorrowerPhone: tc.stored}, tc.given)
			if err != tc.wantErr {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseBorrowForm(t *testing.T) {
	valid := func() map[string][]string {
		return map[string][]string{
			"borrower_name":        {"Asha"},
			"borrower_phone":       {"9876543210"},
			"item_name":            {"Multimeter"},
			"lab_location":         {"Main Lab"},
			"quantity_borrowed":    {"2"},
			"expected_return_date": {"2026-10-05"},
		}
	}
	with := func(key, value string) map[string][]string {
		v := valid()
		v[key] = []string{value}
		return v
	}

	cases := []struct {
		name    string
		values  map[string][]string
		wantErr bool
	}{
		{name: "valid", values: valid()},
		{name: "blank name", values: with("borrower_name", "   "), wantErr: true},
		{name: "blank phone", values: with("borrower_phone", ""), wantErr: true},
		{name: "phone without digits", values: with("borrower_phone", "call me"), wantErr: true},
		{name: "blank item", values: with("item_name", " "), wantErr: true},
		{name: "unparseable date", values: with("expected_return_date", "next tuesday"), wantErr: true},
		{name: "timestamp date is accepted", values: with("expected_return_date", "2026-10-05T00:00:00Z")},
		{name: "zero quantity", values: with("quantity_borrowed", "0"), wantErr: true},
		{name: "negative quantity", values: with("quantity_borrowed", "-3"), wantErr: true},
		{name: "fractional quantity", values: with("quantity_borrowed", "1.5"), wantErr: true},
		{name: "missing quantity", values: with("quantity_borrowed", ""), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := parseBorrowForm(tc.values)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("accepted %+v", req)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if req.Purpose != "Not specified" {
				t.Errorf("purpose = %q, want the default", req.Purpose)
			}
		})
	}
}

func TestExtendReturnDate(t *testing.T) {
	cases := []struct {
		name    string
		current string
		days    int
		hours   int
		want    string
		wantErr bool
	}{
		{name: "two days", current: "2026-10-03", days: 2, want: "2026-10-05"},
		{name: "a few hours rounds up to a day", current: "2026-10-03", hours: 3, want: "2026-10-04"},
		{name: "a day and some hours", current: "2026-10-03", days: 1, hours: 1, want: "2026-10-05"},
		{name: "exactly 24 hours", current: "2026-10-03", hours: 24, want: "2026-10-04"},
		{name: "legacy timestamp", current: "2026-10-03T00:00:00Z", days: 1, want: "2026-10-04"},
		{name: "zero extension", current: "2026-10-03", wantErr: true},
		{name: "negative days", current: "2026-10-03", days: -1, hours: 30, wantErr: true},
		{name: "negative hours", current: "2026-10-03", days: 2, hours: -1, wantErr: true},
		{name: "bad stored date", current: "soon", days: 1, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extendReturnDate(tc.current, tc.days, tc.hours)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoginLimiter(t *testing.T) {
	start := mustTime(t, "2026-10-03T10:00:00Z")

	cases := []struct {
		name        string
		failures    int
		succeed     bool
		checkAfter  time.Duration
		wantBlocked bool
	}{
		{name: "a few mistakes are fine", failures: 3},
		{name: "one short of the limit", failures: loginMaxFailures - 1},
		{name: "at the limit is blocked", failures: loginMaxFailures, wantBlocked: true},
		{name: "still blocked inside the window", failures: loginMaxFailures, checkAfter: 10 * time.Minute, wantBlocked: true},
		{name: "unblocked once the window passes", failures: loginMaxFailures, checkAfter: loginWindow + time.Second},
		{name: "a success clears the count", failures: loginMaxFailures - 1, succeed: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := newLoginLimiter()
			for i := 0; i < tc.failures; i++ {
				l.fail("10.0.0.1", start)
			}
			if tc.succeed {
				l.succeed("10.0.0.1")
				l.fail("10.0.0.1", start)
			}
			if got := l.blocked("10.0.0.1", start.Add(tc.checkAfter)); got != tc.wantBlocked {
				t.Errorf("blocked = %v, want %v", got, tc.wantBlocked)
			}
			// Another client is never caught by someone else's failures
			if l.blocked("10.0.0.2", start.Add(tc.checkAfter)) {
				t.Error("unrelated IP is blocked")
			}
		})
	}
}

func TestLoginLimiterSweepsExpiredEntries(t *testing.T) {
	start := mustTime(t, "2026-10-03T10:00:00Z")
	l := newLoginLimiter()
	for i := 0; i < 100; i++ {
		l.fail(strings.Repeat("x", i+1), start)
	}
	l.fail("late", start.Add(loginWindow+time.Minute))
	if len(l.attempts) != 1 {
		t.Errorf("%d entries kept after the window, want 1", len(l.attempts))
	}
}

func TestPhonesMatch(t *testing.T) {
	// The cancel endpoint used to compare raw strings, so a stray space
	// locked people out of their own booking.
	if !phonesMatch(" 98765 43210 ", "9876543210") {
		t.Error("whitespace should not matter")
	}
	if phonesMatch("abc", "def") {
		t.Error("numbers without digits must not match")
	}
}
