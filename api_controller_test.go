package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func TestSubscriptionEndpointCustomNamesAndFilename(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nodes.yaml":
			w.Header().Set("Content-Disposition", `attachment; filename="provider.yaml"`)
			w.Header().Set("Subscription-Userinfo", "upload=0; download=1073741824; total=10737418240; expire=1788220800")
			_, _ = w.Write([]byte("proxies:\n  - name: HK 01\n    type: ss\n    server: 127.0.0.1\n    port: 10001\n    cipher: aes-128-gcm\n    password: test\n"))
		case "/script.js":
			_, _ = w.Write([]byte("function rulesets(register) {}\nfunction buildConfig(config, legacyRelay) { config['proxy-groups'] = []; config['rules'] = config['rules'].concat(['MATCH,DIRECT']); }"))
		case "/template.yaml":
			_, _ = w.Write([]byte("mode: rule\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cfg := defaultAppConfig()
	cfg.Server.AccessToken = "test-token"

	query := url.Values{}
	query.Add("sub", upstream.URL+"/nodes.yaml")
	query.Add("subName", "Premium A")
	query.Set("script", upstream.URL+"/script.js")
	query.Set("template", upstream.URL+"/template.yaml")
	query.Set("token", cfg.Server.AccessToken)
	query.Set("filename", "My Combined.yml")

	request := httptest.NewRequest(http.MethodGet, "/sub?"+query.Encode(), nil)
	response := httptest.NewRecorder()
	router := setupRouter(cfg)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Disposition"); got != "attachment; filename*=UTF-8''My%20Combined.yaml" {
		t.Fatalf("unexpected Content-Disposition: %q", got)
	}
	for _, fragment := range []string{"[Premium A] HK 01", "Premium A | 已用 1.0 GB / 10.0 GB", "MATCH,DIRECT"} {
		if !strings.Contains(response.Body.String(), fragment) {
			t.Errorf("response does not contain %q:\n%s", fragment, response.Body.String())
		}
	}

	for _, endpoint := range []string{"/example/script.js", "/example/template.yaml"} {
		exampleRequest := httptest.NewRequest(http.MethodGet, endpoint, nil)
		exampleResponse := httptest.NewRecorder()
		router.ServeHTTP(exampleResponse, exampleRequest)
		if exampleResponse.Code != http.StatusOK || exampleResponse.Body.Len() == 0 {
			t.Errorf("embedded example %s is unavailable", endpoint)
		}
	}
}

func TestConfiguredMixedSubscriptionMatchesFullURL(t *testing.T) {
	clearConfigEnv(t)
	const script = `function rulesets(register) {}
function buildConfig(config, legacyRelay) {
  config['proxy-groups'] = [{name:'Select',type:'select',proxies:config.proxies.map(function(p) { return p.name; })}];
  config.rules = ['MATCH,Select'];
  config['allow-lan'] = legacyRelay;
}`
	const template = "mode: rule\n"
	var fetches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/nodes":
			fetches.Add(1)
			w.Header().Set("Subscription-Userinfo", "upload=0; download=1024; total=4096; expire=1788220800")
			_, _ = w.Write([]byte("proxies:\n  - name: AirportNode\n    type: ss\n    server: 127.0.0.1\n    port: 10001\n    cipher: aes-128-gcm\n    password: test\n"))
		case "/script":
			_, _ = w.Write([]byte(script))
		case "/template":
			_, _ = w.Write([]byte(template))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	for filename, content := range map[string]string{"script.js": script, "template.yaml": template} {
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	configFile := writeTestConfig(t, dir, fmt.Sprintf(`server:
  access_token: private-access-token
subscription:
  filename: Mixed.yml
  legacy_relay: true
  script: ./script.js
  template: ./template.yaml
  sources:
    - name: Airport
      url: '%s/nodes?secret=private-source-token'
    - name: Self
      url: '%s?security=reality&sni=www.example.org&pbk=%s&sid=0123&flow=xtls-rprx-vision#DirectNode'
`, upstream.URL, testVLESSBase, testRealityKey))
	cfg, err := loadAppConfig(configFile)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previousWriter := gin.DefaultWriter
	gin.DefaultWriter = &logs
	t.Cleanup(func() { gin.DefaultWriter = previousWriter })
	router := setupRouter(cfg)
	for _, query := range []string{"", "?token=wrong"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sub"+query, nil))
		if response.Code != http.StatusUnauthorized || fetches.Load() != 0 {
			t.Fatal("unauthorized request must fail before fetching sources")
		}
	}
	short := httptest.NewRecorder()
	router.ServeHTTP(short, httptest.NewRequest(http.MethodGet, "/sub?token=private-access-token", nil))
	if short.Code != http.StatusOK {
		t.Fatalf("short URL failed: %s", short.Body.String())
	}
	query := url.Values{
		"token": {cfg.Server.AccessToken}, "script": {upstream.URL + "/script"}, "template": {upstream.URL + "/template"},
		"filename": {cfg.Subscription.Filename}, "legacyRelay": {"true"},
	}
	for _, source := range cfg.Subscription.Sources {
		query.Add("sub", source.URL)
		query.Add("subName", source.Name)
	}
	full := httptest.NewRecorder()
	router.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/sub?"+query.Encode(), nil))
	if full.Code != http.StatusOK || full.Body.String() != short.Body.String() {
		t.Fatalf("short/full URL output differs: status=%d\n%s", full.Code, full.Body.String())
	}
	if short.Header().Get("Content-Disposition") != "attachment; filename*=UTF-8''Mixed.yaml" ||
		short.Header().Get("Subscription-Userinfo") != "upload=0; download=1024; total=4096; expire=1788220800" {
		t.Fatalf("unexpected output headers: %v", short.Header())
	}
	var output map[string]any
	if err := yaml.Unmarshal(short.Body.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output["allow-lan"] != true {
		t.Fatal("legacy_relay did not reach JS")
	}
	groups := output["proxy-groups"].([]any)
	if len(groups) != 2 || len(groups[0].(map[string]any)["proxies"].([]any)) != 1 {
		t.Fatal("only the airport should have subscription statistics")
	}
	if !strings.Contains(short.Body.String(), "[Self] DirectNode") || !strings.Contains(short.Body.String(), "reality-opts:") {
		t.Fatal("direct node did not reach output and policy group")
	}
	for _, secret := range []string{"private-access-token", "private-source-token", "00000000-0000-4000-8000-000000000001"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("access log disclosed credentials")
		}
	}
}

func TestVLESSOnlyShortSubscriptionHasNoEmptyInfoGroup(t *testing.T) {
	cfg := defaultAppConfig()
	cfg.Server.AccessToken = "test"
	cfg.Subscription.Sources = []SubscriptionSource{{URL: testVLESSBase + "#OnlyNode"}}
	cfg.Subscription.script = configResource{Content: `function rulesets(register) {}
function buildConfig(c) { c['proxy-groups'] = [{name:'Select',type:'select',proxies:[c.proxies[0].name]}]; c.rules=['MATCH,Select']; }`}
	cfg.Subscription.template = configResource{Content: "mode: rule\n"}
	response := httptest.NewRecorder()
	setupRouter(cfg).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sub?token=test", nil))
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if strings.Contains(response.Body.String(), SubInfoGroupName) || response.Header().Get("Subscription-Userinfo") != "" {
		t.Fatal("single VLESS source must not create fake statistics or an empty group")
	}
}

func TestSubscriptionModesAreIsolated(t *testing.T) {
	cfg := defaultAppConfig()
	cfg.Subscription.Sources = []SubscriptionSource{{URL: testVLESSBase}}
	for _, query := range []url.Values{
		{"filename": {"override"}}, {"script": {"./private.js"}}, {"subName": {"override"}}, {"legacyRelay": {"false"}},
		{"sub": {testVLESSBase}},
		{"sub": {testVLESSBase}, "script": {"./private.js"}, "template": {"https://example.org/template"}},
		{"sub": {testVLESSBase}, "script": {"https://example.org/script"}, "template": {"file:///private.yaml"}},
		{"sub": {""}, "script": {"https://example.org/script"}, "template": {"https://example.org/template"}},
		{"sub": {testVLESSBase + "?type=xhttp"}, "script": {"https://example.org/script"}, "template": {"https://example.org/template"}},
		{"sub": {testVLESSBase}, "script": {"https://example.org/script"}, "template": {"https://example.org/template"}, "legacyRelay": {"bad"}},
	} {
		if _, err := resolveSubscription(query, cfg.Subscription); err == nil {
			t.Errorf("expected invalid or mixed request to fail: %v", query)
		}
	}
	if _, err := resolveSubscription(url.Values{}, SubscriptionConfig{}); err == nil {
		t.Fatal("missing server sources must fail")
	}
	full, err := resolveSubscription(url.Values{
		"sub": {testVLESSBase + "#NewNode"}, "script": {"https://example.org/script"},
		"template": {"https://example.org/template"}, "legacy_relay": {"1"},
	}, cfg.Subscription)
	if err != nil || !full.LegacyRelay || len(full.Sources) != 1 || full.Sources[0].URL == cfg.Subscription.Sources[0].URL {
		t.Fatalf("full URL must use its own parameters: %#v, %v", full, err)
	}
}
