package main

import (
	"context"
	"flag"
	"fmt"
	"go-sample-application/pkg/config"
	"go-sample-application/pkg/logger"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"

	"golang.org/x/net/netutil"
	"golang.org/x/xerrors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/pyroscope-io/client/pyroscope"
	otelpyroscope "github.com/pyroscope-io/otel-profiling-go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer      trace.Tracer
	serviceName = "go-sample-application"

	inFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "A gauge of requests currently being served by the wrapped handler.",
	})

	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Count of all HTTP requests",
	}, []string{"handler", "code", "method"})

	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "Duration of all HTTP requests",
	}, []string{"handler", "code", "method"})

	responseSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "response_size_bytes",
			Help:    "A histogram of response sizes for requests.",
			Buckets: []float64{200, 500, 900, 1500},
		},
		[]string{},
	)
)

func initializeTraceProvider(ctx context.Context, pyroscopeServer string) (func(context.Context) error, error) {
	r, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil, xerrors.Errorf("failed to create resource: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, xerrors.Errorf("failed to create trace exporter: %w", err)
	}

	// For debugging
	// w := os.Stdout
	// exporter, err := stdouttrace.New(
	// 	stdouttrace.WithWriter(w),
	// 	// Use human-readable output.
	// 	stdouttrace.WithPrettyPrint(),
	// 	// Do not print timestamps for the demo.
	// 	stdouttrace.WithoutTimestamps(),
	// )
	// if err != nil {
	// 	return nil, xerrors.Errorf("failed to create trace exporter: %w", err)
	// }

	sdk := sdktrace.NewTracerProvider(
		sdktrace.WithResource(r),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter)),
	)
	// We wrap the tracer provider to also annotate goroutines with Span ID so
	// that pprof would add corresponding labels to profiling samples.
	otel.SetTracerProvider(otelpyroscope.NewTracerProvider(sdk,
		otelpyroscope.WithAppName(serviceName),
		otelpyroscope.WithPyroscopeURL(pyroscopeServer),
		otelpyroscope.WithRootSpanOnly(true),
		otelpyroscope.WithAddSpanName(true),
		otelpyroscope.WithProfileURL(true),
		otelpyroscope.WithProfileBaselineURL(true),
	))
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	// Finally, set the tracer that can be used for this package.
	tracer = sdk.Tracer(serviceName)

	return sdk.Shutdown, nil
}

func loggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := logger.NewRequestLogger(r.Context(), false)
		ctx := config.SetLogger(r.Context(), logger)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestMiddleware(name string, handler http.HandlerFunc) http.Handler {

	getExemplarFn := func(ctx context.Context) prometheus.Labels {
		if spanCtx := trace.SpanFromContext(ctx); spanCtx.SpanContext().IsSampled() {
			return prometheus.Labels{"TraceID": spanCtx.SpanContext().TraceID().String()}
		}
		return nil
	}

	return loggerMiddleware(
		promhttp.InstrumentHandlerInFlight(
			inFlight,
			promhttp.InstrumentHandlerDuration(
				httpRequestDuration.MustCurryWith(prometheus.Labels{"handler": name}),
				promhttp.InstrumentHandlerCounter(
					httpRequestsTotal.MustCurryWith(prometheus.Labels{"handler": name}),
					promhttp.InstrumentHandlerResponseSize(
						responseSize,
						handler,
						promhttp.WithExemplarFromContext(getExemplarFn),
					),
					promhttp.WithExemplarFromContext(getExemplarFn),
				),
				promhttp.WithExemplarFromContext(getExemplarFn),
			),
		),
	)
}

func main() {
	ctx := context.Background()
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(5)

	address := flag.String("address", "0.0.0.0:8080", "Address to listen on")
	maxConnections := flag.Int("max-connections", 65536, "Maximum numberof connections")
	profiling := flag.Bool("profiling", false, "Enable profiling")
	pyroscopeServer := flag.String("pyroscope-server", "", "Pyroscope server address")
	flag.Parse()

	// if *pyroscopeServer != "" {
	// 	// These 2 lines are only required if you're using mutex or block profiling
	// 	// Read the explanation below for how to set these rates:
	// 	runtime.SetMutexProfileFraction(5)
	// 	runtime.SetBlockProfileRate(5)
	// 	pyroscope.Start(pyroscope.Config{
	// 		ApplicationName: serviceName,

	// 		// replace this with the address of pyroscope server
	// 		ServerAddress: *pyroscopeServer,

	// 		// you can disable logging by setting this to nil
	// 		Logger: pyroscope.StandardLogger,

	// 		// optionally, if authentication is enabled, specify the API key:
	// 		// AuthToken: os.Getenv("PYROSCOPE_AUTH_TOKEN"),

	// 		ProfileTypes: []pyroscope.ProfileType{
	// 			// these profile types are enabled by default:
	// 			pyroscope.ProfileCPU,
	// 			pyroscope.ProfileAllocObjects,
	// 			pyroscope.ProfileAllocSpace,
	// 			pyroscope.ProfileInuseObjects,
	// 			pyroscope.ProfileInuseSpace,

	// 			// these profile types are optional:
	// 			pyroscope.ProfileGoroutines,
	// 			pyroscope.ProfileMutexCount,
	// 			pyroscope.ProfileMutexDuration,
	// 			pyroscope.ProfileBlockCount,
	// 			pyroscope.ProfileBlockDuration,
	// 		},
	// 	})
	// }

	shutdown, err := initializeTraceProvider(ctx, *pyroscopeServer)
	if err != nil {
		log.Fatalf("failed to initialize trace provider: %+v", err.Error())
	}

	mux := http.NewServeMux()
	if *profiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("debug/pprof/trace", pprof.Trace)
	}

	mux.Handle("/", requestMiddleware("top", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})))
	mux.HandleFunc("/readyz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	mux.Handle("/debug-log", requestMiddleware("debug-log", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		config.GetLogger(r.Context()).Errorf("print a severity:error log message")
		config.GetLogger(r.Context()).Infof("print a severity:info log message")
		config.GetLogger(r.Context()).Debugf("print a severity:debug log message")
		spanCtx := trace.SpanFromContext(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"trace_id":%s,"span_id":%s}`, spanCtx.SpanContext().TraceID().String(), spanCtx.SpanContext().SpanID().String())))
	})))
	mux.Handle("/spend-cpu", requestMiddleware("spend-cpu", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		pyroscope.TagWrapper(context.Background(), pyroscope.Labels("controller", "spend_cpu_controller"), func(c context.Context) {
			ctx, span := otel.GetTracerProvider().Tracer(serviceName).Start(c, "SpendCPU")
			defer span.End()

			// Spend some time on CPU.
			d := time.Duration(200+rand.Intn(200)) * time.Millisecond
			begin := time.Now()
			for {
				if time.Now().Sub(begin) > d {
					break
				}
			}
			spanCtx := trace.SpanFromContext(ctx)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"trace_id":%s,"span_id":%s}`, spanCtx.SpanContext().TraceID().String(), spanCtx.SpanContext().SpanID().String())))
		})
	})))

	r := prometheus.NewRegistry()
	r.MustRegister(httpRequestsTotal)
	r.MustRegister(httpRequestDuration)
	r.MustRegister(inFlight)
	r.MustRegister(responseSize)
	mux.Handle("/metrics", promhttp.HandlerFor(
		r,
		promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		}))

	server := &http.Server{
		Handler: otelhttp.NewHandler(mux, serviceName),
	}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatalf("failed to listen: %+v", err.Error())
	}

	go func() {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("panic: %+v\n%s", err, debug.Stack())
			}
		}()
		err := server.Serve(netutil.LimitListener(listener, *maxConnections))
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to serve: %+v", err.Error())
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Printf("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(10)*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("failed to shutdown server: %+v", err.Error())
	}
	if err := shutdown(ctx); err != nil {
		log.Fatalf("failed to shutdown trace provider: %+v", err.Error())
	}
	select {
	case <-ctx.Done():
		log.Printf("timout of 10 seconds.")
	default:
	}
	log.Printf("Server gracefully stopped")
}
