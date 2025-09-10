package provider

import (
	"fmt"
	"net/http"
	"web_proxy_cache/provider/demo"
	"web_proxy_cache/provider/netbox"
)

var Providers = map[string]http.HandlerFunc{
	fmt.Sprintf("/%s/", netbox.Netbox): netbox.Endpoint,
	fmt.Sprintf("/%s/", demo.Demo):     demo.Endpoint,
}
