package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
)

type Logger struct {
	*slog.Logger
	mu    sync.Mutex
	attrs []slog.Attr
}

var (
	defaultLogger *Logger
	once          sync.Once
)

func New(level string, format string, output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level:       parseLevel(level),
		AddSource:   true,
		ReplaceAttr: replaceAttr,
	}

	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(output, opts)
	case "text":
		handler = slog.NewTextHandler(output, opts)
	default:
		handler = slog.NewJSONHandler(output, opts)
	}

	return &Logger{Logger: slog.New(handler)}
}

func Init(level string, format string, output io.Writer) *Logger {
	once.Do(func() {
		defaultLogger = New(level, format, output)
		slog.SetDefault(defaultLogger.Logger)
	})

	return defaultLogger
}

// SetDefault replaces the default global logger instance.
func SetDefault(l *Logger) {
	defaultLogger = l
	if l != nil && l.Logger != nil {
		slog.SetDefault(l.Logger)
	}
}

func Get() *Logger {
	if defaultLogger == nil {
		return Init("info", "json", os.Stdout)
	}
	return defaultLogger
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.SourceKey {
		source := a.Value.Any().(*slog.Source)
		if source != nil {
			_, file, line, ok := runtime.Caller(0)
			if ok {
				source.File = file
				source.Line = line
			}
		}
	}
	return a
}

func (l *Logger) With(args ...any) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	newArgs := make([]any, 0, len(l.attrs)*2+len(args))
	for _, a := range l.attrs {
		newArgs = append(newArgs, a.Key, a.Value.Any())
	}
	newArgs = append(newArgs, args...)
	return &Logger{Logger: l.Logger.With(newArgs...), attrs: l.attrs}
}

type ContextKey string

const (
	TraceIDKey ContextKey = "trace_id"
	SpanIDKey  ContextKey = "span_id"
)

func (l *Logger) WithContext(ctx context.Context) *Logger {
	traceID := getTraceID(ctx)
	spanID := getSpanID(ctx)
	if traceID == "" && spanID == "" {
		return l
	}
	return l.With("trace_id", traceID, "span_id", spanID)
}

func getTraceID(ctx context.Context) string {
	if val, ok := ctx.Value(TraceIDKey).(string); ok && val != "" {
		return val
	}
	if val, ok := ctx.Value("trace_id").(string); ok {
		return val
	}
	return ""
}

func getSpanID(ctx context.Context) string {
	if val, ok := ctx.Value(SpanIDKey).(string); ok && val != "" {
		return val
	}
	if val, ok := ctx.Value("span_id").(string); ok {
		return val
	}
	return ""
}

func Debug(msg string, args ...any) {
	Get().Debug(msg, args...)
}

func Info(msg string, args ...any) {
	Get().Info(msg, args...)
}

func Warn(msg string, args ...any) {
	Get().Warn(msg, args...)
}

func Error(msg string, args ...any) {
	Get().Error(msg, args...)
}

func DebugContext(ctx context.Context, msg string, args ...any) {
	Get().WithContext(ctx).Debug(msg, args...)
}

func InfoContext(ctx context.Context, msg string, args ...any) {
	Get().WithContext(ctx).Info(msg, args...)
}

func WarnContext(ctx context.Context, msg string, args ...any) {
	Get().WithContext(ctx).Warn(msg, args...)
}

func ErrorContext(ctx context.Context, msg string, args ...any) {
	Get().WithContext(ctx).Error(msg, args...)
}
