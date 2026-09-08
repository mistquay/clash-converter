package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Server       ServerConfig       `yaml:"server"`
	Subscription SubscriptionConfig `yaml:"subscription"`
}

type ServerConfig struct {
	Listen         string `yaml:"listen"`
	AccessToken    string `yaml:"access_token"`
	DBPath         string `yaml:"db_path"`
	CacheExpireSec int64  `yaml:"cache_expire_sec"`
}

type SubscriptionConfig struct {
	Filename    string               `yaml:"filename"`
	LegacyRelay bool                 `yaml:"legacy_relay"`
	Script      string               `yaml:"script"`
	Template    string               `yaml:"template"`
	Sources     []SubscriptionSource `yaml:"sources"`
	script      configResource
	template    configResource
}

type SubscriptionSource struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// Local and embedded resources are read once at startup. Remote resources are
// fetched at subscription refresh time, as in the original URL mode.
type configResource struct {
	URL     string
	Content string
}

func (r configResource) read() (string, error) {
	if r.URL != "" {
		return FetchString(r.URL)
	}
	return r.Content, nil
}

func defaultAppConfig() AppConfig {
	return AppConfig{Server: ServerConfig{
		Listen: ":8080", DBPath: "./data/database.db", CacheExpireSec: 86400,
	}, Subscription: SubscriptionConfig{
		script:   configResource{Content: exampleScript},
		template: configResource{Content: exampleTemplate},
	}}
}

func loadAppConfig(filename string) (AppConfig, error) {
	cfg := defaultAppConfig()
	baseDir := "."
	if filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			return cfg, fmt.Errorf("read configuration file: %w", err)
		}
		decoder := yaml.NewDecoder(strings.NewReader(string(data)))
		decoder.KnownFields(true)
		if err = decoder.Decode(&cfg); err != nil {
			// YAML decoder errors can contain scalar values, including credentials.
			return cfg, errors.New("invalid YAML configuration: check field names and value types")
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			return cfg, errors.New("configuration must contain exactly one YAML document")
		}
		baseDir = filepath.Dir(filename)
	}
	if value, ok := os.LookupEnv("ACCESS_TOKEN"); ok {
		cfg.Server.AccessToken = value
	}
	if value, ok := os.LookupEnv("DB_PATH"); ok {
		cfg.Server.DBPath = value
	}
	if value, ok := os.LookupEnv("CACHE_EXPIRE_SEC"); ok {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return cfg, errors.New("CACHE_EXPIRE_SEC must be an integer")
		}
		cfg.Server.CacheExpireSec = seconds
	}
	_, port, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return cfg, errors.New("server.listen must have the form host:port or :port")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return cfg, errors.New("server.listen port must be between 1 and 65535")
	}
	if strings.TrimSpace(cfg.Server.DBPath) == "" {
		return cfg, errors.New("server.db_path must not be empty")
	}
	if cfg.Server.CacheExpireSec < 0 || cfg.Server.CacheExpireSec > int64((1<<63-1)/time.Second) {
		return cfg, errors.New("server.cache_expire_sec is outside the supported range")
	}
	if len(cfg.Subscription.Sources) > 0 && strings.TrimSpace(cfg.Server.AccessToken) == "" {
		return cfg, errors.New("server.access_token or ACCESS_TOKEN is required for configured subscription sources")
	}
	for i := range cfg.Subscription.Sources {
		source := &cfg.Subscription.Sources[i]
		source.URL = strings.TrimSpace(source.URL)
		if err := validateSource(source.URL); err != nil {
			return cfg, fmt.Errorf("subscription.sources[%d]: %w", i, err)
		}
	}
	cfg.Subscription.script, err = loadConfigResource(cfg.Subscription.Script, exampleScript, baseDir)
	if err != nil {
		return cfg, fmt.Errorf("subscription.script: %w", err)
	}
	cfg.Subscription.template, err = loadConfigResource(cfg.Subscription.Template, exampleTemplate, baseDir)
	if err != nil {
		return cfg, fmt.Errorf("subscription.template: %w", err)
	}
	return cfg, nil
}

func loadConfigResource(ref, builtin, baseDir string) (configResource, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return configResource{Content: builtin}, nil
	}
	if strings.Contains(ref, "://") {
		if err := validateHTTPURL(ref); err != nil {
			return configResource{}, err
		}
		return configResource{URL: ref}, nil
	}
	if !filepath.IsAbs(ref) {
		ref = filepath.Join(baseDir, ref)
	}
	data, err := os.ReadFile(ref)
	if err != nil {
		return configResource{}, errors.New("cannot read local file; check the path and Docker volume mount")
	}
	return configResource{Content: string(data)}, nil
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("expected a valid HTTP or HTTPS URL")
	}
	return nil
}

func validateSource(raw string) error {
	if strings.HasPrefix(strings.ToLower(raw), "vless://") {
		_, err := parseVLESS(raw)
		return err
	}
	if err := validateHTTPURL(raw); err != nil {
		return errors.New("source must be an HTTP(S) Clash subscription or a vless:// share link")
	}
	return nil
}
