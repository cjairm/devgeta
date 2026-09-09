package logger

import (
	"go.uber.org/zap"
)

var log *zap.SugaredLogger

// Init initializes the global logger
func Init(verbose bool) {
	var zapLogger *zap.Logger
	var err error

	if verbose {
		zapLogger, err = zap.NewDevelopment()
	} else {
		// Production logger with ERROR level to suppress INFO/DEBUG logs
		cfg := zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
		zapLogger, err = cfg.Build()
	}

	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}

	log = zapLogger.Sugar()
}

// SetForTest swaps the global logger and returns a function that puts the
// previous one back, so a test can assert on what was logged and at which
// level. Pair it with zaptest/observer.
//
// Level is not cosmetic here: an operator reads a `dg install` run by its
// error lines, and a routine outcome logged as an error reads as a broken
// install. That distinction is worth a test, and a test cannot see it without
// a seam — see internal/commands' TestExecCommandLogsMissingBinaryAtDebug.
func SetForTest(l *zap.SugaredLogger) func() {
	previous := log
	log = l
	return func() { log = previous }
}

// L returns the global logger instance
func L() *zap.SugaredLogger {
	if log == nil {
		panic("logger not initialized. Call logger.Init() first.")
	}
	return log
}
