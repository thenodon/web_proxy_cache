package main

import (
	"net/http"
	"net/url"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

var cacheHits = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: MetricsPrefix + "cache_hits_total",
		Help: "Cache hits",
	},
	[]string{"proxy"},
)
var cacheMiss = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: MetricsPrefix + "cache_miss_total",
		Help: "Cache misses",
	},
	[]string{"proxy"},
)
var cacheGraceFetches = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: MetricsPrefix + "cache_grace_fetches_total",
		Help: "Cache grace fetches",
	},
	[]string{"proxy"},
)
var cacheExpire = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: MetricsPrefix + "cache_expire_total",
		Help: "Cache expire",
	},
	[]string{"proxy"},
)

type CacheData struct {
	RequestHeaders  http.Header
	ResponseHeaders http.Header
	Data            interface{}
}

type cacheObj struct {
	lastUsed    time.Time
	ttl         time.Time
	usedCounter int64
	graceTime   time.Time
	cacheData   CacheData
}

type Element struct {
	Value     string
	Timestamp time.Time
}

type SortedSet struct {
	set []Element
	mu  sync.RWMutex
}

func (s *SortedSet) Add(element Element) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if the element already exists in the set
	for _, v := range s.set {
		if v.Value == element.Value {
			return
		}
	}

	// Add the element and sort the set
	s.set = append(s.set, element)
	sort.Slice(s.set, func(i, j int) bool {
		return s.set[i].Timestamp.Before(s.set[j].Timestamp)
	})
}

func (s *SortedSet) Remove(element Element) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, v := range s.set {
		if v.Value == element.Value {
			s.set = append(s.set[:i], s.set[i+1:]...)
			return
		}
	}
}

func (s *SortedSet) Contains(element Element) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, v := range s.set {
		if v.Value == element.Value {
			return true
		}
	}
	return false
}

func (s *SortedSet) Elements() []Element {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.set
}

type Cache struct {
	name     string
	mu       sync.RWMutex
	entries  map[string]*cacheObj
	index    SortedSet
	maxSize  int
	maxTTL   int64
	maxGrace int64
	//fetchFunc func(w http.ResponseWriter, r *http.Request)
	fetchFunc func(r *http.Request)
}

// func NewCache(config ConfigProxy, name string, fetchfunc func(w http.ResponseWriter, r *http.Request)) *Cache {
func NewCache(config ConfigProxy, name string, fetchfunc func(r *http.Request)) *Cache {
	return &Cache{
		entries:   make(map[string]*cacheObj),
		index:     SortedSet{},
		maxSize:   config.CacheSize,
		maxTTL:    config.CacheTTL,
		maxGrace:  config.CacheGrace,
		fetchFunc: fetchfunc,
		name:      name,
	}
}

func (u *Cache) Set(key string, data CacheData) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if len(u.entries) >= u.maxSize {
		oldest := u.index.Elements()[0]
		delete(u.entries, oldest.Value)
		u.index.Remove(oldest)
		log.WithFields(log.Fields{"operation": "cache", "key": oldest}).
			Info("cache size limit reached")
	}

	obj := cacheObj{
		lastUsed:    time.Time{},
		ttl:         time.Now().Add(time.Duration(u.maxTTL) * time.Second),
		usedCounter: 0,
		graceTime:   time.Now().Add(time.Duration(u.maxGrace+u.maxTTL) * time.Second),
		cacheData:   data,
	}
	u.entries[key] = &obj
	u.index.Add(Element{Value: key, Timestamp: time.Now()})
}

func (u *Cache) GetUsage(key string) (int64, time.Time, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	_, exists := u.entries[key]
	if exists {
		return u.entries[key].usedCounter, u.entries[key].lastUsed, true
	}
	return 0, time.Time{}, false
}

func (u *Cache) Inc(key string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.entries[key].usedCounter++
	u.entries[key].lastUsed = time.Now()
}

func (u *Cache) Exists(key string) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	_, exists := u.entries[key]

	return exists
}

func (u *Cache) Delete(key string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if _, exists := u.entries[key]; exists {
		delete(u.entries, key)
		u.index.Remove(Element{Value: key})
		log.WithFields(log.Fields{"operation": "cache", "key": key}).
			Info("cache entry removed")
	}
}

func (u *Cache) Get(key string) (interface{}, bool) {

	u.mu.RLock()
	//defer u.mu.Unlock()
	if value, ok := u.entries[key]; ok {
		u.mu.RUnlock()
		if value.ttl.Before(time.Now()) {
			if value.graceTime.After(time.Now()) && value.usedCounter > 0 {
				url, _ := url.Parse(key)
				url.Host = ""
				url.Scheme = ""

				r := &http.Request{
					Method: http.MethodGet,
					URL:    url,
					Header: u.entries[key].cacheData.RequestHeaders,
				}
				//w := NewCustomResponseWriter()
				go u.fetchFunc(r)
				cacheGraceFetches.WithLabelValues(u.name).Inc()
				log.WithFields(log.Fields{"operation": "cache", "key": key, "used": u.entries[key].usedCounter}).
					Info("TTL expired, grace time")
			} else {
				u.mu.Lock()
				delete(u.entries, key)
				u.index.Remove(Element{Value: key})
				u.mu.Unlock()
				log.WithFields(log.Fields{"operation": "cache", "key": key}).
					Info("TTL expired")
				cacheExpire.WithLabelValues(u.name).Inc()
				log.WithFields(log.Fields{"operation": "cache", "key": key}).
					Info("TTL expired, entry removed")
				return nil, false
			}
		}
		// re-sort the index
		u.mu.Lock()
		u.index.Remove(Element{Value: key})
		u.index.Add(Element{Value: key, Timestamp: time.Now()})
		u.mu.Unlock()
		u.Inc(key)
		cacheHits.WithLabelValues(u.name).Inc()
		log.WithFields(log.Fields{"operation": "cache", "key": key, "used": u.entries[key].usedCounter}).
			Info("Cache hit")
		return value.cacheData, true
	}
	cacheMiss.WithLabelValues(u.name).Inc()
	log.WithFields(log.Fields{"operation": "cache", "key": key}).
		Info("Cache miss")
	u.mu.RUnlock()
	return nil, false
}
