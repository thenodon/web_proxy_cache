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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/ksuid"
	log "github.com/sirupsen/logrus"
)

var version = "undefined"

const (
	Netbox = "netbox"

	// MetricsPrefix the prefix for all internal metrics
	MetricsPrefix = "network_proxy_"
)

var customTransport = http.DefaultTransport

var config = ConfigProxy{
	ProxyLimit:    GetEnvAsInt("LIMIT", 1000),
	CacheUse:      GetEnvAsBool("CACHE_USE", true),
	CacheTTL:      GetEnvAsInt64("CACHE_TTL", 600),
	CacheGrace:    GetEnvAsInt64("CACHE_GRACE", 300),
	CacheSize:     GetEnvAsInt("CACHE_SIZE", 1000),
	ServerAddress: GetEnv("SERVER_ADDRESS", ":8080"),
}

var cache map[string]*Cache

func init() {
	cache = make(map[string]*Cache)
	cache[Netbox] = NewCache(config, Netbox, getForwardContent)
	// Here, you can customize the transport, e.g., set timeouts or enable/disable keep-alive
}

type HandlerInit struct {
	config ConfigProxy
}

func main() {

	versionFlag := flag.Bool("v", false, "Show version")
	flag.Parse()
	if *versionFlag {
		fmt.Printf("web_proxy_cache version %s\n", version)
		os.Exit(0)
	}

	// Create a Prometheus histogram for response time of the exporter
	responseTime := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    MetricsPrefix + "request_duration_seconds",
		Help:    "Histogram of the time (in seconds) each request took to complete.",
		Buckets: []float64{0.050, 0.100, 0.200, 0.500, 0.800, 1.00, 2.000, 3.000},
	},
		[]string{"proxy", "status"},
	)

	// Create a new HTTP server with the netbox function as the handler
	// Setup handler for aci destinations
	path := fmt.Sprintf("/%s/", Netbox)
	http.Handle(path, logCall(promMonitor(http.HandlerFunc(NetboxEndpoint), responseTime, path)))

	// Setup handler for exporter metrics
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))

	server := &http.Server{
		//ReadTimeout:  viper.GetDuration("httpserver.read_timeout") * time.Second,
		//WriteTimeout: viper.GetDuration("httpserver.write_timeout") * time.Second,
		Addr: config.ServerAddress,
	}

	// Start the server and log any errors
	//log.WithFields(log.Fields{"address": config.ServerAddress, "version": version}).Info("Starting proxy server")
	log.WithFields(log.Fields{"address": config.ServerAddress, "version": version, "cache_size": config.CacheSize,
		"cache_ttl": config.CacheTTL, "cache_grace": config.CacheGrace}).Info("Starting proxy server")
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

type NetboxResponse struct {
	Count    int           `json:"count"`
	Next     string        `json:"next"`
	Previous string        `json:"previous"`
	Results  []interface{} `json:"results"`
	//RequestHeaders  http.Header   `json:"RequestHeaders"`
}
