package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"time"
)

//go:embed templates/* static/* dist/*
var EmbeddedFiles embed.FS

// TemplateEngine manages compiled Go HTML templates with custom helper functions.
type TemplateEngine struct {
	templates map[string]*template.Template
}

func NewTemplateEngine() (*TemplateEngine, error) {
	funcMap := template.FuncMap{
		"formatBytes": func(bytes int64) string {
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
		"formatClock": func(t time.Time) string {
			if t.IsZero() {
				return "--:--:--"
			}
			return t.Format("15:04:05")
		},
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "Never"
			}
			return t.UTC().Format("2006-01-02 15:04:05 UTC")
		},
		"formatRelativeTime": func(t time.Time) string {
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
		"calcPercent": func(used, total int64) float64 {
			if total <= 0 {
				return 0.0
			}
			pct := (float64(used) / float64(total)) * 100.0
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
		"settings.html",
	}

	partials := []string{
		"templates/partials/sidebar.html",
		"templates/partials/header.html",
		"templates/partials/telemetry.html",
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

	return &TemplateEngine{templates: tmplMap}, nil
}

func (e *TemplateEngine) Render(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %s not found", name)
	}
	return t.ExecuteTemplate(w, "base.html", data)
}

func (e *TemplateEngine) RenderPartial(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("partial template %s not found", name)
	}
	return t.Execute(w, data)
}

func (e *TemplateEngine) FileServer() http.Handler {
	return http.FileServer(http.FS(EmbeddedFiles))
}
