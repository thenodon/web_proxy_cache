web_proxy_cache
-----------------------

# Overview
The web_proxy_cache is web proxy that is used to front web services that use pagination. For these kind of sites
it is not always possible to get everything without iterate over the returned result.
With the web_proxy_cache it will do it for you before the result is returned to the client to make sure all
data is returned.
The web_proxy_cache will also cache the result for a certain amount of time to make sure the same request is not
overloading the target or that the response time will take to long. It also supports additional fetch from the target if
the data in the cache is expired but in the defined grace period.

# License
Licensed under GPLv3, see LICENSE for details.

# Use case
- Using the Infinity datasource in Grafana that minimal paginating capability and no caching.
- Service discovery in Prometheus that needs to fetch a large amount of data. 

# Supported providers
- Netbox
- Demo - this is just a demo provider that can be used to test the web_proxy_cache and
work as a template for other providers.

> Currently, only Netbox is supported, but it is straightforward to add more targets using a new fetcher and parser.
> If you develop a new provider that can be useful for others, please submit a PR.

# How to use
A call to the web_proxy_cache is done by calling the proxy in the following way, example is Netbox:
```shell
 curl -H "Authorization: Token $NETBOX_TOKEN" -H "X-Forwarded-Host: https://netbox.foo.com" "localhost:8080/netbox/api/dcim/devices/?site=labs&status=active&has_primary_ip=true" 
```
- The X-Forwarded-Host is used to tell the proxy what target to use. 
- The first part of the URL is the provider identity, `netbox` and the rest is the path and query to be sent to the 
target.
> Do not use `limit` and `offset` in the query or other paging technics, this is the responsibility of the provider
> to handle.


# Configuration
Every configuration is done by environment variables.
- `SERVER_ADDRESS` - the port to run the proxy on, default `:8080`

Provider specific environment variables: 
- `<PROVIDER>_LIMIT` - the max size of pagination, default `1000`
- `<PROVIDER>_CACHE_TTL` - the time to keep data in the cache, default `600` seconds
- `<PROVIDER>_CACHE_GRACE` - the time to after TTL where the cache will return cached data but fetch new in the background, default `300` seconds
- `<PROVIDER>_CACHE_SIZE` - max cache size, default `1000`

> For any other providers the configuration is the same just replace `NETBOX` with the provider name.

# Internal metrics
The web_proxy_cache will expose internal metrics on the `/metrics` endpoint. 

# Caching logic
The caching logic is based on the following principles:
- The cache will store the result of the request for a certain amount of time, defined by `<PROVIDER>_CACHE_TTL`.
- If the request is made after the `<PROVIDER>_CACHE_TTL` but within the `<PROVIDER>_CACHE_GRACE`, the cache will return the cached data and
  fetch new data in the background.
- If the request is made after the `<PROVIDER>_CACHE_GRACE`, a full fetch will be done and the cache will be updated with the new data.
- The cache will use the full URL as the key, including query parameters, to ensure that different requests are cached separately.
- The cache will use a LRU (Least Recently Used) strategy to evict old entries when the cache size exceeds `<PROVIDER>_CACHE_SIZE`.

# Implement a new provider
To implement a new provider, create a new fetcher and parser. The fetcher will be used to fetch the data from the target
and the parser will be used to parse the data into a format that can be used by Grafana.

# Netbox provider specific
## Service discovery 
The web_proxy_cache can be used with http based service discovery in Prometheus. The service discovery can in principle 
be used for any api call for the netbox api, but the exporter is designed to work with the 
`/dcim/devices/` endpoint where the filter return a hugh amount of entries.
To format the output for service discovery use the `X-Forwarded-For` header with the value `service-discovery`.
> The reason for this implementation is that it has been observed that the netbox plugin 
> [netbox-plugin-prometheus-sd](https://github.com/FlxPeters/netbox-plugin-prometheus-sd) 
> will take a vary long time to return the result or even return 500 or 504 (probobly proxy timeout) 
> when the number of devices is large.
> Only use this solution if you have a large number of devices in Netbox and the netbox-plugin-prometheus-sd is not 
> working for you.
> Using the web_proxy_cache for Netbox /dcim/devices/ the following labels are NOT available:
> - `__meta_netbox_model` - this is a label added by the netbox-plugin-prometheus-sd to indicate the endpoint called
> - `__meta_netbox_tenant_group` - this attribute is not available in the `/dcim/devices/` endpoint
> - `__meta_netbox_tenant_group_slug` - this attribute is not available in the `/dcim/devices/` endpoint
> 
> Example of using the service discovery with the web_proxy_cache for a tenant that has 24000 AP devices where the filter
> make it return 14000 devices takes approximately 120 seconds the **first** time. Using the netbox-plugin-prometheus-sd 
> it never returned the result. For smaller collections its been observed that the web_proxy_cache is approximately 10 
> times faster.

The following labels will be created for the service discovery using the /dcim/devices/ endpoint:
- `__meta_netbox_device_type`
- `__meta_netbox_device_type_slug`
- `__meta_netbox_id`
- `__meta_netbox_name`
- `__meta_netbox_oob_ip`
- `__meta_netbox_platform`
- `__meta_netbox_platform_slug`
- `__meta_netbox_primary_ip`
- `__meta_netbox_primary_ip4`
- `__meta_netbox_primary_ip6`
- `__meta_netbox_role`
- `__meta_netbox_role_slug`
- `__meta_netbox_serial`
- `__meta_netbox_site`
- `__meta_netbox_site_slug`
- `__meta_netbox_status`
- `__meta_netbox_tenant`
- `__meta_netbox_tenant_group`
- `__meta_netbox_tenant_group_slug`
- `__meta_netbox_tenant_slug`

And all `custom_fields` defined in the Netbox device type.

