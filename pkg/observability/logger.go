package observability

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog for structured logging
type Logger struct {
	logger zerolog.Logger
}

// LogLevel represents logging level
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

// NewLogger creates a new structured logger
func NewLogger(service string, level LogLevel) *Logger {
	var output io.Writer = os.Stdout
	
	// Pretty console output for development
	if os.Getenv("ENV") == "development" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	// Set log level
	var zeroLevel zerolog.Level
	switch level {
	case LevelDebug:
		zeroLevel = zerolog.DebugLevel
	case LevelInfo:
		zeroLevel = zerolog.InfoLevel
	case LevelWarn:
		zeroLevel = zerolog.WarnLevel
	case LevelError:
		zeroLevel = zerolog.ErrorLevel
	default:
		zeroLevel = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(zeroLevel)

	logger := zerolog.New(output).
		With().
		Timestamp().
		Str("service", service).
		Logger()

	return &Logger{logger: logger}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string) {
	l.logger.Debug().Msg(msg)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logger.Debug().Msgf(format, args...)
}

// Info logs an info message
func (l *Logger) Info(msg string) {
	l.logger.Info().Msg(msg)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logger.Info().Msgf(format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string) {
	l.logger.Warn().Msg(msg)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logger.Warn().Msgf(format, args...)
}

// Error logs an error message
func (l *Logger) Error(msg string, err error) {
	l.logger.Error().Err(err).Msg(msg)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, err error, args ...interface{}) {
	l.logger.Error().Err(err).Msgf(format, args...)
}

// Fatal logs a fatal error and exits
func (l *Logger) Fatal(msg string, err error) {
	l.logger.Fatal().Err(err).Msg(msg)
}

// WithField adds a field to the logger
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{
		logger: l.logger.With().Interface(key, value).Logger(),
	}
}

// WithFields adds multiple fields to the logger
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	logger := l.logger.With()
	for k, v := range fields {
		logger = logger.Interface(k, v)
	}
	return &Logger{logger: logger.Logger()}
}

// Zerolog returns the underlying zerolog.Logger for compatibility
func (l *Logger) Zerolog() zerolog.Logger {
	return l.logger
}
