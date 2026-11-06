package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLocalNodeFileRefreshAndMixedSubscription(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	nodeFile := filepath.Join(dir, "nodes.txt")
	writeNodes := func(content string) {
		t.Helper()
		if err := os.WriteFile(nodeFile, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeNodes("\uFEFF# local nodes\r\n\r\n  " + testSSBase + "#First  \r\n" + testVLESSBase + "#LocalVLESS\r\n")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proxies:\n  - {name: AirportNode, type: ss, server: airport.example.org, port: 80, cipher: aes-128-gcm, password: test}\n"))
	}))
	defer upstream.Close()
	configFile := writeTestConfig(t, dir, fmt.Sprintf(`server:
  access_token: test-token
subscription:
  sources:
    - name: Airport
      url: '%s'
    - name: Local
      file: ./nodes.txt
    - url: '%s#DirectSS'
`, upstream.URL, testSSBase))
	cfg, err := loadAppConfig(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Subscription.Sources[1].File != nodeFile {
		t.Fatal("node file path must be resolved against the configuration directory")
	}
	// Keep integration checks entirely local; the production script also builds
	// its policy groups from config.proxies.
	cfg.Subscription.script = configResource{Content: `function rulesets(register) {}
function buildConfig(c) {
  c['proxy-groups'] = [{name:'Select',type:'select',proxies:c.proxies.map(function(p) { return p.name; })}];
  c.rules = ['MATCH,Select'];
}`}
	cfg.Subscription.template = configResource{Content: "mode: rule\n"}
	router := setupRouter(cfg)
	refresh := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sub?token=test-token", nil))
		return response
	}
	for round := 0; round < 2; round++ {
		expectedLocalName := "[Local] First"
		if round == 1 {
			writeNodes(testSSBase + "#Replacement\n")
			expectedLocalName = "[Local] Replacement"
		}
		response := refresh()
		if response.Code != http.StatusOK {
			t.Fatalf("refresh failed: %s", response.Body.String())
		}
		var output struct {
			Proxies []map[string]any `yaml:"proxies"`
			Groups  []struct {
				Name    string   `yaml:"name"`
				Proxies []string `yaml:"proxies"`
			} `yaml:"proxy-groups"`
		}
		if err := yaml.Unmarshal(response.Body.Bytes(), &output); err != nil {
			t.Fatal(err)
		}
		if len(output.Groups) != 2 || len(output.Groups[0].Proxies) != 1 || len(output.Groups[1].Proxies) != 4-round {
			t.Fatalf("only the airport should have statistics; all real nodes should enter Select: %#v", output.Groups)
		}
		for _, expected := range []string{"[Airport] AirportNode", expectedLocalName, "DirectSS"} {
			if !strings.Contains(response.Body.String(), expected) {
				t.Fatalf("missing merged node %q", expected)
			}
		}
		if round == 1 && (strings.Contains(response.Body.String(), "LocalVLESS") || strings.Contains(response.Body.String(), "[Local] First")) {
			t.Fatal("deleted nodes must disappear without restarting the router")
		}
	}
	writeNodes("# comment\nss://private-secret@host:80\n")
	response := refresh()
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "[Airport] AirportNode") ||
		!strings.Contains(response.Body.String(), "DirectSS") || strings.Contains(response.Body.String(), "[Local]") ||
		strings.Contains(response.Body.String(), "private-secret") {
		t.Fatalf("invalid file must not prevent other sources from updating: %s", response.Body.String())
	}
	if err := os.Remove(nodeFile); err != nil {
		t.Fatal(err)
	}
	if response := refresh(); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "[Airport] AirportNode") ||
		!strings.Contains(response.Body.String(), "DirectSS") || strings.Contains(response.Body.String(), "[Local]") || strings.Contains(response.Body.String(), dir) {
		t.Fatalf("missing file must not prevent other sources from updating: %s", response.Body.String())
	}
	writeNodes(testSSBase + "#Recovered\n")
	if response := refresh(); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "[Local] Recovered") {
		t.Fatalf("repaired file must rejoin the subscription without restarting: %s", response.Body.String())
	}
}

func TestLocalNodeFileOnlyHasNoStatistics(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "nodes.txt")
	if err := os.WriteFile(filename, []byte(testSSBase+"#OnlySS\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := defaultAppConfig()
	cfg.Server.AccessToken = "test"
	cfg.Subscription.Sources = []SubscriptionSource{{File: filename}}
	cfg.Subscription.script = configResource{Content: "function buildConfig(c) { c.rules = ['MATCH,DIRECT']; }"}
	cfg.Subscription.template = configResource{Content: "mode: rule\n"}
	response := httptest.NewRecorder()
	setupRouter(cfg).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sub?token=test", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "OnlySS") ||
		strings.Contains(response.Body.String(), SubInfoGroupName) || response.Header().Get("Subscription-Userinfo") != "" {
		t.Fatalf("local files must not create subscription statistics: %s", response.Body.String())
	}
}

func TestLocalNodeFileValidationAndURLIsolation(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	filename := filepath.Join(dir, "nodes.txt")
	for _, content := range []string{"", "# comments only\n", "https://example.org/private-secret", "vmess://private-secret", string([]byte{0xff})} {
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		configFile := writeTestConfig(t, dir, "server:\n  access_token: test\nsubscription:\n  sources:\n    - file: ./nodes.txt\n")
		if _, err := loadAppConfig(configFile); err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("invalid node file must fail at startup without credentials: %v", err)
		}
	}
	for _, fields := range []string{"{}", "file: ./missing.txt", "file: ./nodes.txt\n      url: '" + testSSBase + "'"} {
		configFile := writeTestConfig(t, dir, "server:\n  access_token: test\nsubscription:\n  sources:\n    - "+fields+"\n")
		if _, err := loadAppConfig(configFile); err == nil {
			t.Fatal("missing file, missing source and conflicting fields must fail")
		}
	}
	for _, ref := range []string{"./nodes.txt", filename, "file:///etc/passwd"} {
		_, err := resolveSubscription(url.Values{
			"sub": {ref}, "script": {"https://example.org/script"}, "template": {"https://example.org/template"},
		}, defaultAppConfig().Subscription)
		if err == nil {
			t.Fatal("URL parameters must not grant access to local files")
		}
	}
}
