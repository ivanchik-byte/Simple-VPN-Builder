package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ivanchik-byte/Simple-VPN-Builder/internal/controlplane/store"
	"github.com/jackc/pgx/v5/pgtype"
)

//go:embed templates/* static/* dist/*
var EmbeddedFiles embed.FS

// TemplateEngine manages compiled Go HTML templates with custom helper functions.
type TemplateEngine struct {
	templates map[string]*template.Template
}

func NewTemplateEngine() (*TemplateEngine, error) {
	funcMap := template.FuncMap{
		"formatBytes": func(v any) string {
			var bytes int64
			switch val := v.(type) {
			case int64:
				bytes = val
			case int:
				bytes = int64(val)
			case pgtype.Int8:
				if val.Valid {
					bytes = val.Int64
				}
			default:
				if s, ok := v.(fmt.Stringer); ok {
					_ = s
				}
			}
			if bytes <= 0 {
				return "0 B"
			}
			units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
			b := float64(bytes)
			i := int(math.Floor(math.Log(b) / math.Log(1024)))
			if i >= len(units) {
				i = len(units) - 1
			}
			val := b / math.Pow(1024, float64(i))
			if i == 0 {
				return fmt.Sprintf("%d B", bytes)
			}
			return fmt.Sprintf("%.2f %s", val, units[i])
		},
		"formatPercent": func(val float64) string {
			return fmt.Sprintf("%.1f%%", val)
		},
		"formatClock": func(val any) string {
			t := toTime(val)
			if t.IsZero() {
				return "--:--:--"
			}
			return t.Format("15:04:05")
		},
		"formatTime": func(val any) string {
			t := toTime(val)
			if t.IsZero() {
				return "Never"
			}
			return t.UTC().Format("2006-01-02 15:04:05 UTC")
		},
		"formatRelativeTime": func(val any) string {
			t := toTime(val)
			if t.IsZero() {
				return "Never"
			}
			d := time.Since(t)
			if d < time.Minute {
				return fmt.Sprintf("%ds ago", int(d.Seconds()))
			} else if d < time.Hour {
				return fmt.Sprintf("%dm ago", int(d.Minutes()))
			} else if d < 24*time.Hour {
				return fmt.Sprintf("%dh ago", int(d.Hours()))
			}
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
		},
		"calcPercent": func(usedVal, totalVal any) float64 {
			var used, total float64
			switch v := usedVal.(type) {
			case pgtype.Int8:
				if v.Valid {
					used = float64(v.Int64)
				}
			case int64:
				used = float64(v)
			case int:
				used = float64(v)
			case float64:
				used = v
			}

			switch v := totalVal.(type) {
			case pgtype.Int8:
				if v.Valid {
					total = float64(v.Int64)
				}
			case int64:
				total = float64(v)
			case int:
				total = float64(v)
			case float64:
				total = v
			}

			if total <= 0 {
				return 0.0
			}
			pct := (used / total) * 100.0
			if pct > 100.0 {
				return 100.0
			}
			return pct
		},
		"statusColor": func(status string) string {
			switch status {
			case "online", "active":
				return "text-emerald-500 bg-emerald-500/10 border-emerald-500/20"
			case "degraded", "warning":
				return "text-amber-500 bg-amber-500/10 border-amber-500/20"
			case "offline", "inactive", "revoked":
				return "text-rose-500 bg-rose-500/10 border-rose-500/20"
			default:
				return "text-zinc-400 bg-zinc-500/10 border-zinc-500/20"
			}
		},
		"stringUUID": func(u any) string {
			return fmt.Sprintf("%v", u)
		},
		"stringVal": func(v any) string {
			switch val := v.(type) {
			case pgtype.Text:
				if val.Valid {
					return val.String
				}
				return ""
			case string:
				return val
			default:
				return fmt.Sprintf("%v", v)
			}
		},
		"hasProtocol": func(protocols []string, proto string) bool {
			target := strings.ToLower(strings.TrimSpace(proto))
			for _, p := range protocols {
				if strings.ToLower(strings.TrimSpace(p)) == target {
					return true
				}
			}
			return false
		},
		"formatNumeric": func(v any) string {
			switch val := v.(type) {
			case pgtype.Numeric:
				if val.Valid {
					f, _ := val.Float64Value()
					if f.Valid {
						return fmt.Sprintf("%.2f", f.Float64)
					}
				}
				return "0.00"
			default:
				return fmt.Sprintf("%v", v)
			}
		},
		"stringDiff": func(b []byte) string {
			return string(b)
		},
		"parseRBACDiff": func(b []byte) *store.AuditRBACDiffPayload {
			if len(b) == 0 {
				return nil
			}
			var p store.AuditRBACDiffPayload
			if err := json.Unmarshal(b, &p); err == nil && (p.TargetAdminEmail != "" || p.Status != "" || len(p.Changes) > 0) {
				return &p
			}
			return nil
		},
		"isJSON": func(b []byte) bool {
			var js any
			return json.Unmarshal(b, &js) == nil
		},
		"minus": func(a, b int) int {
			return a - b
		},
		"plus": func(a, b int) int {
			return a + b
		},
	}


	pages := []string{
		"login.html",
		"dashboard.html",
		"nodes.html",
		"node_detail.html",
		"users.html",
		"plans.html",
		"credentials.html",
		"analytics.html",
		"audit.html",
		"settings.html",
		"broadcast.html",
	}

	partials := []string{
		"templates/partials/sidebar.html",
		"templates/partials/header.html",
		"templates/partials/telemetry.html",
		"templates/partials/ai_drawer.html",
	}

	tmplMap := make(map[string]*template.Template)

	for _, page := range pages {
		pagePath := "templates/pages/" + page
		files := append([]string{"templates/base.html", pagePath}, partials...)
		t, err := template.New(page).Funcs(funcMap).ParseFS(EmbeddedFiles, files...)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", page, err)
		}
		tmplMap[page] = t
	}

	// Also compile standalone partials for HTMX swaps
	htmxPartials := []string{
		"telemetry_swap.html",
		"telemetry_cards.html",
		"node_status_swap.html",
		"users_table_swap.html",
	}
	for _, p := range htmxPartials {
		path := "templates/partials/" + p
		t, err := template.New(p).Funcs(funcMap).ParseFS(EmbeddedFiles, path)
		if err == nil {
			tmplMap[p] = t
		}
	}

	// Standalone pages (that do not inherit base.html)
	standalonePages := []string{
		"404.html",
		"error.html",
	}
	for _, sp := range standalonePages {
		path := "templates/pages/" + sp
		t, err := template.New(sp).Funcs(funcMap).ParseFS(EmbeddedFiles, path)
		if err == nil {
			tmplMap[sp] = t
		}
	}

	return &TemplateEngine{templates: tmplMap}, nil
}

func (e *TemplateEngine) Render(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %s not found", name)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base.html", data); err != nil {
		return fmt.Errorf("failed to render template %s: %w", name, err)
	}
	_, err := buf.WriteTo(w)
	return err
}

func (e *TemplateEngine) RenderStandalone(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("standalone template %s not found", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render standalone template %s: %w", name, err)
	}
	_, err := buf.WriteTo(w)
	return err
}

func (e *TemplateEngine) RenderPartial(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("partial template %s not found", name)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render partial template %s: %w", name, err)
	}
	_, err := buf.WriteTo(w)
	return err
}

func (e *TemplateEngine) FileServer() http.Handler {
	return http.FileServer(http.FS(EmbeddedFiles))
}

func toTime(val any) time.Time {
	switch v := val.(type) {
	case time.Time:
		return v
	case *time.Time:
		if v != nil {
			return *v
		}
	case pgtype.Timestamptz:
		if v.Valid {
			return v.Time
		}
	case pgtype.Timestamp:
		if v.Valid {
			return v.Time
		}
	}
	return time.Time{}
}

func FaviconBytes() *strings.Reader {
	b, err := EmbeddedFiles.ReadFile("static/favicon.svg")
	if err != nil {
		return strings.NewReader("")
	}
	return strings.NewReader(string(b))
}

func FormatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	b := float64(bytes)
	i := int(math.Floor(math.Log(b) / math.Log(1024)))
	if i >= len(units) {
		i = len(units) - 1
	}
	val := b / math.Pow(1024, float64(i))
	if i == 0 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.2f %s", val, units[i])
}
