package logging

import (
	// todo: maybe use zerolog instead for performance?
	"go.uber.org/zap"
)

type loggerKeyType struct{}

var (
	loggerKey = loggerKeyType{}
)

type Logger struct {
	*zap.Logger
	fields []zap.Field
}

func NewLogger(name string) *Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	return &Logger{Logger: logger.Named(name), fields: []zap.Field{}}
}

func (l *Logger) With(fields ...zap.Field) *Logger {
	newFields := make([]zap.Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)

	return &Logger{Logger: l.Logger.With(newFields...), fields: newFields}
}

func (l *Logger) Info(msg string, additionalFields ...zap.Field) {
	fields := make([]zap.Field, len(l.fields)+len(additionalFields))
	copy(fields, l.fields)
	copy(fields[len(l.fields):], additionalFields)

	l.Logger.Info(msg, fields...)
}

func (l *Logger) Fatal(msg string, additionalFields ...zap.Field) {
	fields := make([]zap.Field, len(l.fields)+len(additionalFields))
	copy(fields, l.fields)
	copy(fields[len(l.fields):], additionalFields)

	l.Logger.Fatal(msg, fields...)
}

func (l *Logger) Panic(msg string, additionalFields ...zap.Field) {
	fields := make([]zap.Field, len(l.fields)+len(additionalFields))
	copy(fields, l.fields)
	copy(fields[len(l.fields):], additionalFields)

	l.Logger.Panic(msg, fields...)
}

func (l *Logger) Error(msg string, additionalFields ...zap.Field) {
	fields := make([]zap.Field, len(l.fields)+len(additionalFields))
	copy(fields, l.fields)
	copy(fields[len(l.fields):], additionalFields)

	l.Logger.Error(msg, fields...)
}

func (l *Logger) Warn(msg string, additionalFields ...zap.Field) {
	fields := make([]zap.Field, len(l.fields)+len(additionalFields))
	copy(fields, l.fields)
	copy(fields[len(l.fields):], additionalFields)

	l.Logger.Warn(msg, fields...)
}

func (l *Logger) Debug(msg string, additionalFields ...zap.Field) {
	fields := make([]zap.Field, len(l.fields)+len(additionalFields))
	copy(fields, l.fields)
	copy(fields[len(l.fields):], additionalFields)

	l.Logger.Debug(msg, fields...)
}
