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

func Init(level string, format string, output io.Writer) *Logger {
	once.Do(func() {
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

		defaultLogger = &Logger{Logger: slog.New(handler)}
		slog.SetDefault(defaultLogger.Logger)
	})

	return defaultLogger
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

func (l *Logger) WithContext(ctx context.Context) *Logger {
	return l.With("trace_id", getTraceID(ctx), "span_id", getSpanID(ctx))
}

func getTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value("trace_id").(string); ok {
		return traceID
	}
	return ""
}

func getSpanID(ctx context.Context) string {
	if spanID, ok := ctx.Value("span_id").(string); ok {
		return spanID
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