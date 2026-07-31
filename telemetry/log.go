// SPDX-License-Identifier: TODO

package telemetry

import (
	"sync"

	"github.com/m4schini/splitkauf/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

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

func Logger(names ...string) *zap.Logger {
	initLogger()
	l := zap.L()
	for _, name := range names {
		l = l.Named(name)
	}

	return l
}
