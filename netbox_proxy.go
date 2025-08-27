package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// Devices
	DeviceType      = "device_type"
	DeviceTypeSlug  = "device_type_slug"
	ID              = "id"
	Model           = "model"
	Name            = "name"
	OobIP           = "oob_ip"
	Platform        = "platform"
	PlatformSlug    = "platform_slug"
	PrimaryIP       = "primary_ip"
	PrimaryIP4      = "primary_ip4"
	PrimaryIP6      = "primary_ip6"
	Role            = "role"
	RoleSlug        = "role_slug"
	Serial          = "serial"
	Site            = "site"
	SiteSlug        = "site_slug"
	Status          = "status"
	Tenant          = "tenant"
	TenantGroup     = "tenant_group"
	TenantGroupSlug = "tenant_group_slug"
	TenantSlug      = "tenant_slug"
)

func NetboxEndpoint(w http.ResponseWriter, r *http.Request) {

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

	netboxCache(w, r)
}

func netboxCache(w http.ResponseWriter, r *http.Request) {
	r.URL.Path = strings.TrimPrefix(r.URL.Path, fmt.Sprintf("/%s", Netbox))

	key := fmt.Sprintf("%s%s?%s", r.Header.Get("X-Forwarded-Host"), r.URL.Path, r.URL.RawQuery)

	var cacheData interface{}
	var ok bool
	cacheData, ok = cache[Netbox].Get(key)

	if !ok {
		errorText, status, err := getForwardContentData(r)
		if err != nil {
			http.Error(w, errorText, status)
			return
		}
		cacheData, ok = cache[Netbox].Get(key)
		if !ok {
			http.Error(w, "Not found in cache", http.StatusNotFound)
			return
		}
	}

	// create the response headers from the cache data
	for name, values := range cacheData.(CacheData).ResponseHeaders {

		for _, value := range values {
			if name != "Content-Length" && name != "Allow" {
				w.Header().Add(name, value)
			}
		}
	}
	w.Header().Add("Allow", "GET")

	countUsed, lastUsed, ok := cache[Netbox].GetUsage(key)
	if ok {
		w.Header().Add("X-Proxy-Cache", strconv.FormatInt(countUsed, 10))
		w.Header().Add("X-Proxy-Cache-Last-Used", lastUsed.Format("2006-01-02 15:04:05"))
	}

	// Set the status code of the original response to the status code of the proxy response
	w.WriteHeader(http.StatusOK)

	// If the request is for service discovery, call the service discovery function
	if r.Header.Get("X-Forwarded-For") == "service-discovery" {

		doServiceDiscovery(w, cacheData)

		return
	}
	// Encode the response body to JSON and write it to the original response
	err := json.NewEncoder(w).Encode(cacheData.(CacheData).Data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func doServiceDiscovery(w http.ResponseWriter, cacheData interface{}) {
	sd, err := serviceDiscovery(cacheData.(CacheData).Data)
	if err != nil {
		logrus.WithFields(logrus.Fields{"operation": "service-discovery", "error": err}).Error("Service discovery failed")
		http.Error(w, "Service discovery failed", http.StatusInternalServerError)
		return
	}
	err = json.NewEncoder(w).Encode(sd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	return
}

func serviceDiscovery(cacheData interface{}) ([]map[string]interface{}, error) {
	logrus.WithFields(logrus.Fields{"operation": "service-discovery"}).Info("Service discovery called")

	raw, ok := cacheData.(NetboxResponse)
	if !ok {
		return nil, fmt.Errorf("data is not a map[string]interface{}")
	}

	results := raw.Results
	if !ok {
		return nil, fmt.Errorf("missing or malformed 'results' field in JSON data")
	}

	var sd []map[string]interface{}
	for _, entry := range results {
		labelsMap := make(map[string]string)
		device, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		// Top-level fields
		target := device[Name].(string)
		labelsMap[addMeta(Name)], _ = device[Name].(string)
		labelsMap[addMeta(Serial)], _ = device[Serial].(string)
		labelsMap[addMeta(ID)] = strconv.FormatFloat(device[ID].(float64), 'f', -1, 64)

		if data, ok := device[DeviceType].(map[string]interface{}); ok {
			labelsMap[addMeta(DeviceType)], _ = data["model"].(string)
			labelsMap[addMeta(DeviceTypeSlug)], _ = data["slug"].(string)
		}

		if data, ok := device[Site].(map[string]interface{}); ok {
			labelsMap[addMeta(Site)], _ = data["name"].(string)
			labelsMap[addMeta(SiteSlug)], _ = data["slug"].(string)

		}

		if data, ok := device[Role].(map[string]interface{}); ok {
			labelsMap[addMeta(Role)], _ = data["name"].(string)
			labelsMap[addMeta(RoleSlug)], _ = data["slug"].(string)
		}

		if data, ok := device[Platform].(map[string]interface{}); ok {
			labelsMap[addMeta(Platform)], _ = data["name"].(string)
			labelsMap[addMeta(PlatformSlug)], _ = data["slug"].(string)
		}

		if data, ok := device[Tenant].(map[string]interface{}); ok {
			labelsMap[addMeta(Tenant)], _ = data["name"].(string)
			labelsMap[addMeta(TenantSlug)], _ = data["slug"].(string)
		}

		if data, ok := device[PrimaryIP].(map[string]interface{}); ok {
			//labelsMap[PrimaryIP], _ = data["address"].(string)
			addr, _ := data["address"].(string)
			labelsMap[addMeta(PrimaryIP)] = strings.SplitN(addr, "/", 2)[0]
		}

		if data, ok := device[PrimaryIP4].(map[string]interface{}); ok {
			addr, _ := data["address"].(string)
			labelsMap[addMeta(PrimaryIP4)] = strings.SplitN(addr, "/", 2)[0]
		}

		if data, ok := device[PrimaryIP6].(map[string]interface{}); ok {
			addr, _ := data["address"].(string)
			labelsMap[addMeta(PrimaryIP6)] = strings.SplitN(addr, "/", 2)[0]
		}

		if data, ok := device[OobIP].(map[string]interface{}); ok {
			addr, _ := data["address"].(string)
			labelsMap[addMeta(OobIP)] = strings.SplitN(addr, "/", 2)[0]
		}

		if data, ok := device[Status].(map[string]interface{}); ok {
			labelsMap[addMeta(Status)], _ = data["value"].(string)
		}

		// Custom fields
		if data, ok := device["custom_fields"].(map[string]interface{}); ok {
			customFields := flattenCustomFields(data)
			for k, v := range customFields {
				// Add custom fields to labelsMap with prefix __meta_netbox_custom_
				labelsMap[addMeta(fmt.Sprintf("custom_field_%s", k))] = v
			}
		}

		sd = append(sd, map[string]interface{}{
			"targets": []string{target},
			"labels":  labelsMap,
		})
	}
	return sd, nil
}

func addMeta(key string) string {
	return fmt.Sprintf("__meta_netbox_%s", key)
}

func flattenCustomFields(customFields map[string]interface{}) map[string]string {
	flat := make(map[string]string)
	for k, v := range customFields {
		if v == nil {
			flat[k] = ""
		} else {
			flat[k] = fmt.Sprintf("%v", v)
		}
	}
	return flat
}

func getForwardContent(r *http.Request) {
	_, _, err := getForwardContentData(r)
	if err != nil {
		logrus.WithFields(logrus.Fields{"operation": "proxy"}).
			Error("pre fetch cache")
		cache[Netbox].Delete(fmt.Sprintf("%s%s?%s", r.Header.Get("X-Forwarded-Host"), r.URL.Path, r.URL.RawQuery))
	}

}

func getForwardContentData(r *http.Request) (string, int, error) {
	// Create a new HTTP request with the same method, URL, and body as the original request
	var result NetboxResponse
	targetURL := r.URL
	// Get the X-Forwarded-Host header from the original request and use it to construct the new URL to the target
	forwardHost := r.Header.Get("X-Forwarded-Host")
	limit := config.ProxyLimit
	offset := 0
	var newUrl string
	if strings.Contains(targetURL.String(), "?") {
		newUrl = fmt.Sprintf("%s%s&limit=%d&offset=%d", forwardHost, targetURL.String(), limit, offset)
	} else {
		newUrl = fmt.Sprintf("%s%s?limit=%d&offset=%d", forwardHost, targetURL.String(), limit, offset)
	}
	proxyReq, err := http.NewRequest(r.Method, newUrl, r.Body)
	if err != nil {
		//http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
		logrus.WithFields(logrus.Fields{"operation": "proxy", "url": newUrl, "err": err, "offset": 0}).
			Error("creating proxy request")
		return "Error creating proxy request", http.StatusInternalServerError, err
	}

	// Copy the RequestHeaders from the original request to the proxy request without the X-Forwarded-Host header
	for name, values := range r.Header {
		for _, value := range values {
			if name != "X-Forwarded-Host" {
				proxyReq.Header.Add(name, value)
			}
		}
	}

	// Send the proxy request using the custom transport
	startTime := time.Now()
	resp, err := customTransport.RoundTrip(proxyReq)
	if err != nil {
		//http.Error(w, fmt.Sprintf("Error sending proxy request"), http.StatusInternalServerError)
		logrus.WithFields(logrus.Fields{"operation": "proxy", "url": proxyReq.URL, "err": err, "offset": 0}).
			Error("sending proxy request")
		return "Error sending proxy request", http.StatusInternalServerError, err
	}
	logrus.WithFields(logrus.Fields{
		"operation": "proxy",
		"url":       proxyReq.URL,
		"offset":    0,
		"exectime":  float64(time.Since(startTime).Milliseconds()),
		"size":      resp.ContentLength,
	}).Info("sending proxy request")

	// Check if the response status code is not OK (200) and return an error with the status code
	// from the target response.
	// Typical status codes are 400 for bad request, 401 for unauthorized, 403 for forbidden, 404 for not found, etc.
	// 400 typically means that the request had bad filters or parameters.
	if resp.StatusCode != http.StatusOK {
		// http.Error(w, "Error sending proxy request", resp.StatusCode)
		logrus.WithFields(logrus.Fields{"operation": "proxy", "url": proxyReq.URL, "offset": 0, "status": resp.StatusCode}).
			Error("response status")
		return "Error sending proxy request", resp.StatusCode, nil
	}

	body, err := io.ReadAll(resp.Body) // response body is []byte

	_ = resp.Body.Close()

	var resultTemp NetboxResponse
	if err := json.Unmarshal(body, &resultTemp); err != nil { // Parse []byte to go struct pointer
		// http.Error(w, "Could not unmarshal", http.StatusInternalServerError)
		logrus.WithFields(logrus.Fields{"operation": "proxy", "url": proxyReq.URL, "offset": 0, "err": err}).
			Error("unmarshall body")
		return "Could not unmarshal", http.StatusInternalServerError, err
	}

	result.Count = resultTemp.Count
	result.Results = append(result.Results, resultTemp.Results...)

	countCollect := 1
	// Run the loop until all the Data is collected based on the initial count
	for countCollect*limit < resultTemp.Count {

		proxyReq, err = http.NewRequest(r.Method, resultTemp.Next, r.Body)
		if err != nil {
			//http.Error(w, "Error creating proxy request", http.StatusInternalServerError)
			logrus.WithFields(logrus.Fields{"operation": "proxy", "err": err, "offset": countCollect}).
				Error("creating proxy request")
			return "Error creating proxy request", http.StatusInternalServerError, err
		}

		// Copy the RequestHeaders from the original request to the proxy request without the X-Forwarded-Host header
		for name, values := range r.Header {
			for _, value := range values {
				if name != "X-Forwarded-Host" {
					proxyReq.Header.Add(name, value)
				}
			}
		}

		// Send the proxy request using the custom transport
		startTime = time.Now()
		resp, err = customTransport.RoundTrip(proxyReq)
		if err != nil {
			//http.Error(w, "Error sending proxy request", http.StatusInternalServerError)
			logrus.WithFields(logrus.Fields{"operation": "proxy", "url": proxyReq.URL, "err": err, "offset": countCollect}).
				Error("sending proxy request")
			return "Error sending proxy request", http.StatusInternalServerError, err
		}
		logrus.WithFields(logrus.Fields{
			"operation": "proxy",
			"url":       proxyReq.URL,
			"offset":    countCollect,
			"exectime":  float64(time.Since(startTime).Milliseconds()),
			"size":      resp.ContentLength,
		}).Info("sending proxy request")

		body, err = io.ReadAll(resp.Body) // response body is []byte
		_ = resp.Body.Close()
		if err := json.Unmarshal(body, &resultTemp); err != nil { // Parse []byte to go struct pointer
			//http.Error(w, "Could not unmarshal", http.StatusInternalServerError)
			logrus.WithFields(logrus.Fields{"operation": "proxy", "url": proxyReq.URL, "offset": countCollect, "err": err}).
				Error("unmarshall body")
			return "Could not unmarshal", http.StatusInternalServerError, err
		}
		result.Count = resultTemp.Count
		result.Results = append(result.Results, resultTemp.Results...)
		countCollect++
	}

	// Encode the response body to JSON and write it to the original response
	//err = json.NewEncoder(w).Encode(result)
	if err != nil {
		//http.Error(w, err.Error(), http.StatusInternalServerError)
		logrus.WithFields(logrus.Fields{"operation": "proxy", "url": proxyReq.URL, "err": err}).
			Error("encode response")
		return err.Error(), http.StatusInternalServerError, err
	}

	cacheData := CacheData{
		RequestHeaders:  r.Header,
		ResponseHeaders: resp.Header,
		Data:            result,
	}

	cache[Netbox].Set(fmt.Sprintf("%s%s?%s", r.Header.Get("X-Forwarded-Host"), r.URL.Path, r.URL.RawQuery), cacheData)
	return "Success", http.StatusOK, nil
}
