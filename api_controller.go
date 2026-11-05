package main

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed ui.html
var uiHTML string

//go:embed example/script.js
var exampleScript string

//go:embed example/template.yaml
var exampleTemplate string

// setupRouter 配置路由
func setupRouter(cfg AppConfig) (r *gin.Engine) {
	r = gin.New()
	// Gin's default access log includes the query string, which contains tokens.
	r.Use(gin.LoggerWithFormatter(func(p gin.LogFormatterParams) string {
		return fmt.Sprintf("[HTTP] %s %s %d %s\n", p.Method, p.Request.URL.Path, p.StatusCode, p.Latency)
	}), gin.Recovery())
	err := r.SetTrustedProxies([]string{"127.0.0.1", "172.17.0.0/16"})
	if err != nil {
		panic(err)
	}

	r.GET("/ping", handlePing)
	r.GET("/sub", func(c *gin.Context) { handleSubscription(c, cfg) })
	r.GET("/ui", handleUI)
	r.GET("/example/script.js", handleExampleScript)
	r.GET("/example/template.yaml", handleExampleTemplate)

	return
}

func handlePing(c *gin.Context) {
	c.String(http.StatusOK, "pong")
}

func handleUI(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, uiHTML)
}

func handleExampleScript(c *gin.Context) {
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", []byte(exampleScript))
}

func handleExampleTemplate(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", []byte(exampleTemplate))
}

// handleSubscription 处理订阅转换请求
// 支持多个订阅合并、流量统计、用量信息显示
func handleSubscription(c *gin.Context, cfg AppConfig) {
	userToken := c.Query("token")

	// 鉴权
	if userToken != cfg.Server.AccessToken {
		L().Warn("Unauthorized request received")
		c.String(http.StatusUnauthorized, "Unauthorized request")
		return
	}

	options, err := resolveSubscription(c.Request.URL.Query(), cfg.Subscription)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}

	// 提取所有订阅的节点
	allProxies := make([]SubscriptionData, 0, len(options.Sources))
	for i, source := range options.Sources {
		name := fmt.Sprintf("订阅%02d", i+1)
		proxies, err := source.read(name)
		if err != nil {
			message := fmt.Sprintf("source %d: %s", i+1, err.Error())
			L().Error(message)
			c.String(http.StatusInternalServerError, message)
			return
		}
		setSubscriptionName(&proxies, source.Name)
		allProxies = append(allProxies, proxies)
	}

	// 合并订阅数据
	mergedProxies := mergeProxies(allProxies)

	// 获取模板和脚本
	template, err := options.template.read()
	if err != nil {
		L().Error(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	script, err := options.script.read()
	if err != nil {
		L().Error(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// 执行 JS 脚本生成配置
	result, err := ExecJs(script, template, mergedProxies, options.LegacyRelay)
	if err != nil {
		L().Error(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// 添加用量信息节点组
	finalResult, err := addSubInfoGroup(result, mergedProxies.SubInfos)
	if err != nil {
		L().Error(err.Error())
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// 设置响应头
	for h, v := range mergedProxies.TransparentHeaders {
		c.Header(h, v)
	}
	if filename := normalizeOutputFilename(options.Filename); filename != "" {
		c.Header("Content-Disposition", fmt.Sprintf(
			"attachment; filename*=UTF-8''%s", url.PathEscape(filename),
		))
	}
	c.String(http.StatusOK, finalResult)
}

func resolveSubscription(query url.Values, configured SubscriptionConfig) (SubscriptionConfig, error) {
	if !query.Has("sub") {
		if len(configured.Sources) == 0 {
			return SubscriptionConfig{}, fmt.Errorf("no server subscription configured; supply sub, script and template")
		}
		for _, key := range []string{"subName", "script", "template", "filename", "legacyRelay", "legacy_relay"} {
			if query.Has(key) {
				return SubscriptionConfig{}, fmt.Errorf("%s cannot override server configuration; use token only or the full URL mode with sub", key)
			}
		}
		return configured, nil
	}
	options := SubscriptionConfig{Filename: query.Get("filename")}
	if query.Get("script") == "" || query.Get("template") == "" {
		return options, fmt.Errorf("sub, script and template are required in full URL mode")
	}
	for _, key := range []string{"script", "template"} {
		if err := validateHTTPURL(query.Get(key)); err != nil {
			return options, fmt.Errorf("%s: %w", key, err)
		}
	}
	// Local files are only accepted from the administrator's configuration file.
	options.script = configResource{URL: query.Get("script")}
	options.template = configResource{URL: query.Get("template")}
	legacyRelayRaw := query.Get("legacyRelay")
	if legacyRelayRaw == "" {
		legacyRelayRaw = query.Get("legacy_relay")
	}
	if legacyRelayRaw != "" {
		value, err := strconv.ParseBool(legacyRelayRaw)
		if err != nil {
			return options, fmt.Errorf("legacyRelay must be a boolean")
		}
		options.LegacyRelay = value
	}
	names := query["subName"]
	for i, raw := range query["sub"] {
		source := SubscriptionSource{URL: strings.TrimSpace(raw)}
		if err := validateSource(source.URL); err != nil {
			return options, fmt.Errorf("source %d: %w", i+1, err)
		}
		if i < len(names) {
			source.Name = names[i]
		}
		options.Sources = append(options.Sources, source)
	}
	return options, nil
}

func normalizeOutputFilename(filename string) string {
	filename = strings.TrimSpace(strings.ReplaceAll(filename, "\\", "/"))
	filename = path.Base(filename)
	if filename == "." || filename == "/" || filename == "" {
		return ""
	}
	if strings.EqualFold(path.Ext(filename), ".yaml") || strings.EqualFold(path.Ext(filename), ".yml") {
		filename = strings.TrimSuffix(filename, path.Ext(filename))
	}
	return filename + ".yaml"
}
