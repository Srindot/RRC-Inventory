package main

// Tests for the inventory rules that need no database, and a check that the
// account, booking and inventory routes can all be registered together (gin
// panics at startup on conflicting paths).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestItemKey(t *testing.T) {
	same := []string{"RealSense D435i", "realsense-d435i", "  REALSENSE d435i ", "RealSense_D435i!"}
	for _, name := range same {
		if itemKey(name) != "realsensed435i" {
			t.Errorf("itemKey(%q) = %q", name, itemKey(name))
		}
	}
	if itemKey("D435") == itemKey("D435i") {
		t.Error("different models must not collide")
	}
	if itemKey("--") != "" {
		t.Error("punctuation alone is no name")
	}
}

func TestRoutesRegisterWithoutConflicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	noop := func(c *gin.Context) {}
	// The handlers are never called here, so no database is needed.
	registerAccountRoutes(api, nil, nil, newLoginLimiter(), noop)
	registerBookingRoutes(api, nil, noop)
	registerInventoryRoutes(api, nil, nil, noop)
	registerThreadRoutes(api, nil, noop)
	registerDashboardRoutes(api, nil, noop)
	if len(router.Routes()) < 30 {
		t.Errorf("only %d routes registered", len(router.Routes()))
	}

	// And the whole router as main builds it, printer routes included.
	if n := len(newRouter(nil, nil, nil, newLoginLimiter()).Routes()); n < 60 {
		t.Errorf("full router has only %d routes", n)
	}
}

// A URL path value handed to GORM as a bare inline condition is run as SQL
// ("/api/return/1 OR 1=1"), so ids must always go through "id = ?".
func TestNoRawPathParamsInQueries(t *testing.T) {
	raw := regexp.MustCompile(`\.(First|Take|Last|Find|Delete)\(\s*&?[A-Za-z_][\w{}]*\s*,\s*(c\.Param\(|loanID\))`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if raw.MatchString(line) {
				t.Errorf("%s:%d passes a path value as a raw condition: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

func TestBorrowReturnDateCap(t *testing.T) {
	far := time.Now().AddDate(2, 0, 0).Format("2006-01-02")
	_, err := parseBorrowForm(map[string][]string{
		"item_id": {"1"}, "quantity_borrowed": {"1"}, "expected_return_date": {far},
	})
	if err == nil {
		t.Error("a return date two years out was accepted")
	}
}
