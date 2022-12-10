package config

import (
	"context"
	"go-sample-application/pkg/logger"
)

type contextKey string

const (
	loggerKey contextKey = "logger"
)

func SetLogger(ctx context.Context, logger logger.ILogger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func GetLogger(ctx context.Context) logger.ILogger {
	return ctx.Value(loggerKey).(logger.ILogger)
}
