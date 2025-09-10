package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"web_proxy_cache/config"
	"web_proxy_cache/proxy_cache"

	"github.com/sirupsen/logrus"
)

const (
	Demo = "demo"
)

var Config = config.ConfigProxy{
	ProxyLimit: config.GetEnvAsInt("DEMO_LIMIT", 1000),
	CacheUse:   config.GetEnvAsBool("DEMO_CACHE_USE", true),
	CacheTTL:   config.GetEnvAsInt64("DEMO_CACHE_TTL", 600),
	CacheGrace: config.GetEnvAsInt64("DEMO_CACHE_GRACE", 300),
	CacheSize:  config.GetEnvAsInt("DEMO_CACHE_SIZE", 1000),
}
var customTransport = http.DefaultTransport

var cache map[string]*proxy_cache.Cache

func init() {
	cache = make(map[string]*proxy_cache.Cache)
	cache[Demo] = proxy_cache.NewCache(Config, Demo, getForwardContent)

}

type proxyResponse struct {
	Entity []interface{} `json:"entity"`
	//RequestHeaders  http.Header   `json:"RequestHeaders"`
}

func Endpoint(w http.ResponseWriter, r *http.Request) {

	// Guard clause to check if the request method is GET
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Guard clause to check if the X-Forwarded-Host header is present in the request
	if r.Header.Get("X-Forwarded-Host") == "" {
		http.Error(w, "X-Forwarded-Host header is required", http.StatusBadRequest)
		return
	}

	cacheHandling(w, r)
}

func cacheHandling(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = strings.TrimPrefix(r.URL.Path, fmt.Sprintf("/%s", Demo))

	key := fmt.Sprintf("%s%s?%s", r.Header.Get("X-Forwarded-Host"), r.URL.Path, r.URL.RawQuery)

	var cacheData interface{}
	var ok bool
	cacheData, ok = cache[Demo].Get(key)

	if !ok {
		errorText, status, err := getForwardContentData(r)
		if err != nil {
			http.Error(w, errorText, status)
			return
		}
		cacheData, ok = cache[Demo].Get(key)
		if !ok {
			http.Error(w, "Not found in proxy_cache", http.StatusNotFound)
			return
		}
	}

	// create the response headers from the proxy_cache data
	for name, values := range cacheData.(proxy_cache.CacheData).ResponseHeaders {

		for _, value := range values {
			if name != "Content-Length" && name != "Allow" {
				w.Header().Add(name, value)
			}
		}
	}
	w.Header().Add("Allow", "GET")

	countUsed, lastUsed, ok := cache[Demo].GetUsage(key)
	if ok {
		w.Header().Add("X-Proxy-Cache", strconv.FormatInt(countUsed, 10))
		w.Header().Add("X-Proxy-Cache-Last-Used", lastUsed.Format("2006-01-02 15:04:05"))
	}

	// Set the status code of the original response to the status code of the proxy response
	w.WriteHeader(http.StatusOK)

	// Encode the response body to JSON and write it to the original response
	err := json.NewEncoder(w).Encode(cacheData.(proxy_cache.CacheData).Data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func getForwardContent(r *http.Request) {
	_, _, err := getForwardContentData(r)
	if err != nil {
		logrus.WithFields(logrus.Fields{"operation": "proxy", "proxy": Demo}).
			Error("pre fetch proxy_cache")
		cache[Demo].Delete(fmt.Sprintf("%s%s?%s", r.Header.Get("X-Forwarded-Host"), r.URL.Path, r.URL.RawQuery))
	}
}

func getForwardContentData(r *http.Request) (string, int, error) {
	// Create a new HTTP request with the same method, URL, and body as the original request
	var result proxyResponse

	// Just add some fake data to the response
	result.Entity = []interface{}{
		map[string]interface{}{"id": 1, "name": "Demo Entity 1"},
		map[string]interface{}{"id": 2, "name": "Demo Entity 2"},
	}

	cacheData := proxy_cache.CacheData{
		RequestHeaders:  r.Header,
		ResponseHeaders: nil,
		Data:            result,
	}

	cache[Demo].Set(fmt.Sprintf("%s%s?%s", r.Header.Get("X-Forwarded-Host"), r.URL.Path, r.URL.RawQuery), cacheData)
	return "Success", http.StatusOK, nil
}
