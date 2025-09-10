package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	config2 "web_proxy_cache/config"
	"web_proxy_cache/provider"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/ksuid"
	log "github.com/sirupsen/logrus"
)

var version = "undefined"

var ServerAddress = config2.GetEnv("SERVER_ADDRESS", ":8080")

func main() {

	versionFlag := flag.Bool("v", false, "Show version")
	flag.Parse()
	if *versionFlag {
		fmt.Printf("web_proxy_cache version %s\n", version)
		os.Exit(0)
	}

	// Create a Prometheus histogram for response time of the exporter
	responseTime := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    config2.MetricsPrefix + "request_duration_seconds",
		Help:    "Histogram of the time (in seconds) each request took to complete.",
		Buckets: []float64{0.050, 0.100, 0.200, 0.500, 0.800, 1.00, 2.000, 3.000},
	},
		[]string{"proxy", "status"},
	)

	// Register each provider endpoint
	for path, handler := range provider.Providers {
		log.WithFields(log.Fields{"path": path}).Info("Registering provider")
		http.Handle(path, logCall(promMonitor(handler, responseTime, path)))
	}

	// Setup handler for exporter metrics
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))

	server := &http.Server{
		//ReadTimeout: viper.GetDuration("httpserver.read_timeout") * time.Second,
		//WriteTimeout: viper.GetDuration("httpserver.write_timeout") * time.Second,
		Addr: ServerAddress,
	}

	// Start the server and log any errors
	//log.WithFields(log.Fields{"address": config.ServerAddress, "version": version}).Info("Starting proxy server")
	log.WithFields(log.Fields{"address": ServerAddress, "version": version}).Info("Starting proxy server")
	//, "cache_size": config.CacheSize, "cache_ttl": config.CacheTTL, "cache_grace": config.CacheGrace}).Info("Starting proxy server")
	err := server.ListenAndServe()
	if err != nil {
		log.Fatal("Error starting proxy server: ", err)
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	length     int
}

func logCall(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()

		lrw := loggingResponseWriter{ResponseWriter: w}
		requestId := nextRequestID()

		ctx := context.WithValue(r.Context(), "requestid", requestId)
		next.ServeHTTP(&lrw, r.WithContext(ctx)) // call original

		w.Header().Set("Content-Length", strconv.Itoa(lrw.length))
		log.WithFields(log.Fields{
			"method":    r.Method,
			"uri":       r.RequestURI,
			"fabric":    r.URL.Query().Get("target"),
			"status":    lrw.statusCode,
			"length":    lrw.length,
			"requestid": requestId,
			"exec_time": time.Since(start).Microseconds(),
		}).Info("api call")
	})
}
func nextRequestID() ksuid.KSUID {
	return ksuid.New()
}

func promMonitor(next http.Handler, ops *prometheus.HistogramVec, endpoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(&lrw, r) // call original
		response := time.Since(start).Seconds()
		ops.With(prometheus.Labels{"proxy": strings.ReplaceAll(endpoint, "/", ""), "status": strconv.Itoa(lrw.statusCode)}).Observe(response)
	})
}
