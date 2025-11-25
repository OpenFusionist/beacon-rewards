package server

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestLoadTemplatesSeparatesPages(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates returned error: %v", err)
	}
	if len(templates) == 0 {
		t.Fatalf("expected templates to be loaded")
	}

	required := []string{"address-rewards.html", "top-deposits.html"}
	for _, name := range required {
		if _, ok := templates[name]; !ok {
			t.Fatalf("template %s not found in loaded set", name)
		}
	}

	addressHTML := renderTemplateToString(t, templates["address-rewards.html"], "address-rewards.html", nil)
	topHTML := renderTemplateToString(t, templates["top-deposits.html"], "top-deposits.html", nil)

	if addressHTML == topHTML {
		t.Fatalf("address template should differ from top-deposits template output")
	}
	if strings.Contains(addressHTML, `<div id="table-container"`) {
		t.Fatalf("address template unexpectedly contains top-deposits table markup")
	}
	if !strings.Contains(addressHTML, "Address rewards lookup") {
		t.Fatalf("address template missing expected address query heading")
	}
}

func TestTopDepositsTableRendersWithdrawalAddress(t *testing.T) {
	templates, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates returned error: %v", err)
	}

	tmpl, ok := templates["top-deposits-table.html"]
	if !ok {
		t.Fatalf("template top-deposits-table.html not found in loaded set")
	}

	depositorAddr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	withdrawalAddr := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	data := map[string]any{
		"results": []map[string]any{
			{
				"depositor_address":              depositorAddr,
				"withdrawal_address":             withdrawalAddr,
				"depositor_label":                "Example Label",
				"total_deposit":                  int64(32000000000),
				"validators_total":               int64(3),
				"active":                         int64(2),
				"slashed":                        int64(0),
				"voluntary_exited":               int64(1),
				"total_active_effective_balance": int64(64000000000),
			},
		},
		"sort_by": "withdrawal_address",
		"order":   "asc",
	}

	rendered := renderTemplateToString(t, tmpl, "top-deposits-table.html", data)

	if strings.Contains(rendered, `data-sort-by="withdrawal_address"`) {
		t.Fatalf("withdrawal column should not be sortable")
	}
	if !strings.Contains(rendered, withdrawalAddr) {
		t.Fatalf("rendered table did not include withdrawal address value")
	}
	if !strings.Contains(rendered, depositorAddr) {
		t.Fatalf("rendered table did not include depositor address value")
	}
}

func renderTemplateToString(t *testing.T, tmpl *template.Template, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if tmpl == nil {
		t.Fatalf("template %s is nil", name)
	}
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		t.Fatalf("failed to execute template %s: %v", name, err)
	}
	return buf.String()
}
