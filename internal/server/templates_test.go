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

	addressHTML := renderTemplateToString(t, templates["address-rewards.html"], "address-rewards.html")
	topHTML := renderTemplateToString(t, templates["top-deposits.html"], "top-deposits.html")

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

func renderTemplateToString(t *testing.T, tmpl *template.Template, name string) string {
	t.Helper()
	var buf bytes.Buffer
	if tmpl == nil {
		t.Fatalf("template %s is nil", name)
	}
	if err := tmpl.ExecuteTemplate(&buf, name, nil); err != nil {
		t.Fatalf("failed to execute template %s: %v", name, err)
	}
	return buf.String()
}
