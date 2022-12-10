package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"go.opentelemetry.io/otel/trace"
)

type ILogger interface {
	Errorf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

type RequestLogger struct {
	ctx         context.Context
	errorLogger *log.Logger
	infoLogger  *log.Logger
	debugLogger *log.Logger
}

func NewRequestLogger(ctx context.Context, verbose bool) *RequestLogger {
	logger := &RequestLogger{}
	logger.ctx = ctx
	logger.errorLogger = log.New(os.Stderr, "", log.LUTC)
	logger.infoLogger = log.New(os.Stdout, "", log.LUTC)
	if verbose {
		logger.debugLogger = log.New(os.Stdout, "", log.LUTC)
	} else {
		logger.debugLogger = log.New(io.Discard, "", log.LUTC)
	}
	return logger
}

func (l *RequestLogger) SetRequestContext(ctx context.Context) {
	l.ctx = ctx
}
func (l *RequestLogger) Errorf(format string, args ...interface{}) {
	spanCtx := trace.SpanFromContext(l.ctx)
	l.errorLogger.Printf(`{"severity":"error","trace_id":"%s","span_id":"%s","body":%q}`, spanCtx.SpanContext().TraceID().String(), spanCtx.SpanContext().SpanID().String(), fmt.Sprintf(format, args...))
}
func (l *RequestLogger) Infof(format string, args ...interface{}) {
	spanCtx := trace.SpanFromContext(l.ctx)
	l.infoLogger.Printf(`{"severity":"info","trace_id":"%s","span_id":"%s","body":%q}`, spanCtx.SpanContext().TraceID().String(), spanCtx.SpanContext().SpanID().String(), fmt.Sprintf(format, args...))
}
func (l *RequestLogger) Debugf(format string, args ...interface{}) {
	spanCtx := trace.SpanFromContext(l.ctx)
	l.debugLogger.Printf(`{"severity":"error","trace_id":"%s","span_id":"%s","body":%q}`, spanCtx.SpanContext().TraceID().String(), spanCtx.SpanContext().SpanID().String(), fmt.Sprintf(format, args...))
}
