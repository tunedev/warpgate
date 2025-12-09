package logging

import (
	"log/slog"
	"os"
)

type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

type SlogLogger struct {
	l *slog.Logger
}

func New() *SlogLogger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	return &SlogLogger{l: slog.New(handler)}
}

func (s *SlogLogger) Info(msg string, args ...any) {
	s.l.Info(msg, args...)
}

func (s *SlogLogger) Error(msg string, args ...any) {
	s.l.Error(msg, args...)
}
