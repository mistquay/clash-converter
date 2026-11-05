package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"resty.dev/v3"
)

// SubscriptionMeta 订阅元信息
type SubscriptionMeta struct {
	Url        string
	Name       string
	Upload     int64
	Download   int64
	Total      int64
	Expire     int64
	StatusCode int
	RawBody    string
}

// SubscriptionData 订阅数据容器，包含节点、透传头和订阅信息
type SubscriptionData struct {
	Proxies            []map[string]any `yaml:"proxies"`
	TransparentHeaders map[string]string
	SubInfos           []*SubscriptionMeta
}

// parseSubscriptionUserinfo 解析 Subscription-Userinfo 头
// 格式: upload=123; download=456; total=789; expire=1234567890
func parseSubscriptionUserinfo(header string) (upload, download, total, expire int64) {
	pairs := strings.Split(header, ";")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		kv := strings.Split(pair, "=")
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		val, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "upload":
			upload = val
		case "download":
			download = val
		case "total":
			total = val
		case "expire":
			expire = val
		}
	}
	return
}

// extractFilename 从 Content-Disposition 头提取文件名
// 实现 RFC 2231
func extractFilename(contentDisposition string) string {
	parts := strings.Split(contentDisposition, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "filename*=") {
			value := strings.TrimPrefix(part, "filename*=")
			if idx := strings.Index(value, "''"); idx != -1 {
				value = value[idx+2:]
			}
			value = strings.Trim(value, "\"")
			decoded, err := url.PathUnescape(value)
			if err == nil {
				value = decoded
			}
			return removeExtension(value)
		}
		if strings.HasPrefix(part, "filename=") {
			value := strings.TrimPrefix(part, "filename=")
			value = strings.Trim(value, "\"")
			return removeExtension(value)
		}
	}
	return ""
}

func removeExtension(filename string) string {
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		return filename[:idx]
	}
	return filename
}

// ExtractProxies 从订阅URL提取节点和元信息
func ExtractProxies(url string, name string) (nodes SubscriptionData, err error) {
	if isShareLink(url) {
		proxy, err := parseShareLink(url)
		if err != nil {
			return nodes, err
		}
		return SubscriptionData{Proxies: []map[string]any{proxy}}, nil
	}
	nodes = SubscriptionData{
		TransparentHeaders: make(map[string]string),
		SubInfos:           make([]*SubscriptionMeta, 0, 1),
	}

	subInfo := &SubscriptionMeta{
		Url:  url,
		Name: name,
	}

	L().Info("Fetching subscription nodes")

	client := resty.New()
	defer func() {
		if closeErr := client.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	req := client.R()
	req.SetHeader("User-Agent", "clash.meta/v1.19.14")

	res, err := req.Get(url)
	if err != nil {
		err = fmt.Errorf("subscription request failed")
		return
	}

	subInfo.StatusCode = res.StatusCode()
	subInfo.RawBody = res.String()

	if res.StatusCode() != 200 {
		err = fmt.Errorf("subscription returned HTTP %d", res.StatusCode())
		return
	}

	err = yaml.Unmarshal(res.Bytes(), &nodes)
	if err != nil {
		err = fmt.Errorf("subscription response is not valid Clash YAML")
		return
	}
	if len(nodes.Proxies) == 0 {
		return nodes, fmt.Errorf("subscription contains no Clash proxies")
	}

	headers := res.Header()

	// 优先使用 Content-Disposition 中的文件名
	contentDisposition := headers.Get("Content-Disposition")
	if contentDisposition != "" {
		filename := extractFilename(contentDisposition)
		if filename != "" {
			subInfo.Name = filename
		}
	}

	for header := range ClashHeaders {
		headerValue := headers.Get(header)
		if headerValue != "" {
			nodes.TransparentHeaders[header] = headerValue
		}
	}

	userinfoHeader := headers.Get("Subscription-Userinfo")
	if userinfoHeader != "" {
		upload, download, total, expire := parseSubscriptionUserinfo(userinfoHeader)
		subInfo.Upload = upload
		subInfo.Download = download
		subInfo.Total = total
		subInfo.Expire = expire
	}

	nodes.SubInfos = append(nodes.SubInfos, subInfo)

	return
}

func setSubscriptionName(nodes *SubscriptionData, name string) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return
	}

	for _, info := range nodes.SubInfos {
		info.Name = name
	}

	usedNames := make(map[string]int, len(nodes.Proxies))
	renames := make(map[string]string, len(nodes.Proxies))
	for _, proxy := range nodes.Proxies {
		oldName, ok := proxy["name"].(string)
		if !ok || strings.TrimSpace(oldName) == "" {
			continue
		}

		cleanName := strings.Join(strings.Fields(oldName), " ")
		baseName := fmt.Sprintf("[%s] %s", name, cleanName)
		usedNames[baseName]++
		newName := baseName
		if usedNames[baseName] > 1 {
			newName = fmt.Sprintf("%s #%d", baseName, usedNames[baseName])
		}
		proxy["name"] = newName
		if _, exists := renames[oldName]; !exists {
			renames[oldName] = newName
		}
	}

	for _, proxy := range nodes.Proxies {
		dialerProxy, ok := proxy["dialer-proxy"].(string)
		if !ok {
			continue
		}
		if renamed, exists := renames[dialerProxy]; exists {
			proxy["dialer-proxy"] = renamed
		}
	}
}

// mergeProxies 合并多个订阅的数据
// 规则：节点顺序合并，流量统计累加，过期时间取最大值
func mergeProxies(allProxies []SubscriptionData) SubscriptionData {
	merged := SubscriptionData{
		Proxies:            make([]map[string]any, 0),
		TransparentHeaders: make(map[string]string),
		SubInfos:           make([]*SubscriptionMeta, 0, len(allProxies)),
	}

	var totalUpload, totalDownload, totalTotal, maxExpire int64
	filenames := make([]string, 0, len(allProxies))

	for _, pc := range allProxies {
		merged.Proxies = append(merged.Proxies, pc.Proxies...)

		for _, info := range pc.SubInfos {
			totalUpload += info.Upload
			totalDownload += info.Download
			totalTotal += info.Total
			if info.Expire > maxExpire {
				maxExpire = info.Expire
			}
			merged.SubInfos = append(merged.SubInfos, info)
			filenames = append(filenames, info.Name)
		}
	}

	if totalTotal > 0 {
		merged.TransparentHeaders["Subscription-Userinfo"] = fmt.Sprintf(
			"upload=%d; download=%d; total=%d; expire=%d",
			totalUpload, totalDownload, totalTotal, maxExpire,
		)
	}

	if len(filenames) > 0 {
		combinedName := strings.Join(filenames, " | ")
		encodedName := url.PathEscape(combinedName)
		merged.TransparentHeaders["Content-Disposition"] = fmt.Sprintf(
			"attachment; filename*=UTF-8''%s", encodedName,
		)
	}

	return merged
}
