package config

const (
	MetricsPrefix = "network_proxy_"
)

type ConfigProxy struct {
	ProxyLimit int   `mapstructure:"proxy_limit"`
	CacheUse   bool  `mapstructure:"use_cache"`
	CacheTTL   int64 `mapstructure:"cache_ttl"`
	CacheGrace int64 `mapstructure:"cache_grace"`
	CacheSize  int   `mapstructure:"cache_size"`
	//ServerAddress string `mapstructure:"server_address"`
}
