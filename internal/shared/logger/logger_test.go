package logger

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger_InitAndLevels(t *testing.T) {
	var buf bytes.Buffer
	log := New("debug", "json", &buf)
	SetDefault(log)
	require.NotNil(t, log)

	Debug("debug message", "key1", "val1")
	Info("info message", "key2", "val2")
	Warn("warn message")
	Error("error message")

	output := buf.String()
	assert.Contains(t, output, "debug message")
	assert.Contains(t, output, "info message")
	assert.Contains(t, output, "warn message")
	assert.Contains(t, output, "error message")
}

func TestLogger_WithContext(t *testing.T) {
	var buf bytes.Buffer
	log := New("info", "json", &buf)
	SetDefault(log)

	ctx := context.WithValue(context.Background(), TraceIDKey, "trace-xyz")
	ctx = context.WithValue(ctx, SpanIDKey, "span-123")

	InfoContext(ctx, "context message")
	DebugContext(ctx, "debug context message")
	WarnContext(ctx, "warn context message")
	ErrorContext(ctx, "error context message")

	output := buf.String()
	assert.Contains(t, output, "trace-xyz")
	assert.Contains(t, output, "span-123")
	assert.Contains(t, output, "context message")
}

func TestLogger_ParseLevel(t *testing.T) {
	assert.Equal(t, parseLevel("debug"), parseLevel("DEBUG"))
	assert.Equal(t, parseLevel("info"), parseLevel("unknown"))
	assert.Equal(t, parseLevel("warn"), parseLevel("warning"))
	assert.Equal(t, parseLevel("error"), parseLevel("ERROR"))
}

func TestLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	log := New("info", "text", &buf)
	SetDefault(log)
	require.NotNil(t, Get())

	Info("plain text log")
	assert.Contains(t, buf.String(), "plain text log")
	assert.True(t, strings.TrimSpace(buf.String()) != "")
}
