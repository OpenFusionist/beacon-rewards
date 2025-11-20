package server

import (
	"html/template"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// loadTemplates loads HTML templates
func loadTemplates() (map[string]*template.Template, error) {
	funcMap := template.FuncMap{
		"formatGweiToAce": func(gwei int64) string {
			ace := float64(gwei) / 1e9
			return formatFloat(ace, 6)
		},
		"formatAddress": func(addr string) string {
			if len(addr) > 10 {
				return addr[:6] + "..." + addr[len(addr)-4:]
			}
			return addr
		},
		"formatNumber": func(n interface{}) string {
			var num int64
			switch v := n.(type) {
			case int64:
				num = v
			case int:
				num = int64(v)
			case uint64:
				num = int64(v)
			default:
				return "0"
			}
			return formatInt(num)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"formatFloat": formatFloat,
	}

	// Try multiple possible paths
	possiblePaths := []string{
		"internal/server/templates/*.html",
		"./internal/server/templates/*.html",
		"templates/*.html",
		"./templates/*.html",
	}

	var allFiles []string
	for _, pattern := range possiblePaths {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		if len(matches) > 0 {
			allFiles = matches
			slog.Info("Found templates", "pattern", pattern, "count", len(matches))
			break
		}
	}

	// If still no files, try to find templates directory
	if len(allFiles) == 0 {
		// Try to find templates relative to current working directory
		wd, err := os.Getwd()
		if err == nil {
			templateDir := filepath.Join(wd, "internal", "server", "templates")
			if info, err := os.Stat(templateDir); err == nil && info.IsDir() {
				pattern := filepath.Join(templateDir, "*.html")
				matches, err := filepath.Glob(pattern)
				if err == nil && len(matches) > 0 {
					allFiles = matches
					slog.Info("Found templates in working directory", "dir", templateDir, "count", len(matches))
				}
			}
		}
	}

	if len(allFiles) == 0 {
		slog.Warn("No template files found")
		return nil, nil
	}

	var baseFile string
	for _, path := range allFiles {
		if filepath.Base(path) == "base.html" {
			baseFile = path
			break
		}
	}

	var baseTemplate *template.Template
	var err error
	if baseFile != "" {
		baseTemplate, err = template.New("base.html").Funcs(funcMap).ParseFiles(baseFile)
		if err != nil {
			slog.Error("Failed to parse base template", "error", err, "file", baseFile)
			return nil, err
		}
	} else {
		slog.Warn("base.html not found; full page templates will be parsed without layout")
	}

	templates := make(map[string]*template.Template)
	for _, path := range allFiles {
		name := filepath.Base(path)
		if name == "base.html" {
			continue
		}

		useBase, err := templateUsesBase(path)
		if err != nil {
			slog.Error("Failed to inspect template for base usage", "file", path, "error", err)
			return nil, err
		}

		switch {
		case useBase && baseTemplate != nil:
			clone, err := baseTemplate.Clone()
			if err != nil {
				slog.Error("Failed to clone base template", "error", err, "file", path)
				return nil, err
			}
			if _, err := clone.ParseFiles(path); err != nil {
				slog.Error("Failed to parse template with base", "file", path, "error", err)
				return nil, err
			}
			templates[name] = clone
		default:
			tmpl, err := template.New(name).Funcs(funcMap).ParseFiles(path)
			if err != nil {
				slog.Error("Failed to parse partial template", "file", path, "error", err)
				return nil, err
			}
			templates[name] = tmpl
		}
	}

	var templateNames []string
	for name := range templates {
		templateNames = append(templateNames, name)
	}
	slog.Info("Loaded templates", "count", len(templateNames), "templates", strings.Join(templateNames, ","))

	return templates, nil
}

func templateUsesBase(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return strings.Contains(string(content), `{{template "base.html" .}}`), nil
}

func formatInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 {
		s = s[1:]
	}
	var result strings.Builder
	length := len(s)
	for i, char := range s {
		if i > 0 && (length-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(char)
	}
	if n < 0 {
		return "-" + result.String()
	}
	return result.String()
}

func formatFloat(f float64, precision int) string {
	// Handle very small numbers
	if math.Abs(f) < 0.000001 && f != 0 {
		return "0"
	}

	// Format with precision
	s := strconv.FormatFloat(f, 'f', precision, 64)

	// Remove trailing zeros
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")

	// Add thousand separators for integer part
	parts := strings.Split(s, ".")
	intPart := parts[0]
	if len(parts) > 1 {
		return formatIntPart(intPart) + "." + parts[1]
	}
	return formatIntPart(intPart)
}

func formatIntPart(s string) string {
	if s == "" || s == "0" {
		return "0"
	}
	var result strings.Builder
	length := len(s)
	for i, char := range s {
		if i > 0 && (length-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(char)
	}
	return result.String()
}
