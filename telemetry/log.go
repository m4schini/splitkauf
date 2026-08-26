// SPDX-License-Identifier: CC0-1.0

// Package telemetry configures the shared zap logger.
package telemetry

import (
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/m4schini/splitkauf/config"
)

// initLogger installs the zap global logger exactly once, on first use of
// Logger.
//
//nolint:gochecknoglobals // package-level logger singleton by design
var initLogger = sync.OnceFunc(func() {
	var cfg zap.Config

	if config.C.App.Debug {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}

	switch config.C.App.LogLevel {
	case "debug":
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "info":
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn":
		cfg.Level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		cfg.Level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	}

	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}

	zap.ReplaceGlobals(logger)
})

// Logger returns the shared zap logger, initializing it on first call, named
// by successively nesting each of names (e.g. Logger("api", "docs")).
func Logger(names ...string) *zap.Logger {
	initLogger()

	l := zap.L()
	for _, name := range names {
		l = l.Named(name)
	}

	return l
}
