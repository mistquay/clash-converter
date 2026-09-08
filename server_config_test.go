package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"ACCESS_TOKEN", "DB_PATH", "CACHE_EXPIRE_SEC"} {
		t.Setenv(key, "") // Registers restoration of the original environment.
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func writeTestConfig(t *testing.T, dir, body string) string {
	t.Helper()
	filename := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestAppConfigDefaultsAndEnvironmentOverrides(t *testing.T) {
	clearConfigEnv(t)
	cfg, err := loadAppConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":8080" || cfg.Server.DBPath != "./data/database.db" || cfg.Server.CacheExpireSec != 86400 {
		t.Fatalf("unexpected defaults: %#v", cfg.Server)
	}
	if cfg.Subscription.script.Content != exampleScript || cfg.Subscription.template.Content != exampleTemplate {
		t.Fatal("missing embedded resource defaults")
	}
	filename := writeTestConfig(t, t.TempDir(), "server:\n  access_token: file-token\n  cache_expire_sec: 20\n  db_path: file.db\nsubscription:\n  sources:\n    - url: '"+testVLESSBase+"'\n")
	t.Setenv("ACCESS_TOKEN", "env-token")
	t.Setenv("DB_PATH", "env.db")
	t.Setenv("CACHE_EXPIRE_SEC", "0")
	cfg, err = loadAppConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.AccessToken != "env-token" || cfg.Server.DBPath != "env.db" || cfg.Server.CacheExpireSec != 0 {
		t.Fatalf("environment did not override YAML: %#v", cfg.Server)
	}
}

func TestAppConfigLoadsLocalResourcesRelativeToConfig(t *testing.T) {
	clearConfigEnv(t)
	dir := t.TempDir()
	script := "function rulesets(register) {}"
	if err := os.WriteFile(filepath.Join(dir, "script.js"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	filename := writeTestConfig(t, dir, "subscription:\n  script: ./script.js\n  template: https://example.org/template.yaml\n")
	cfg, err := loadAppConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Subscription.script.Content != script || cfg.Subscription.template.URL != "https://example.org/template.yaml" {
		t.Fatal("resource resolution failed")
	}
	if err := os.WriteFile(filepath.Join(dir, "script.js"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.Subscription.script.read()
	if err != nil || got != script {
		t.Fatal("local resource should be held until restart")
	}
}

func TestAppConfigRejectsInvalidConfiguration(t *testing.T) {
	clearConfigEnv(t)
	for _, body := range []string{
		"", "server:\n  typo: true\n", "server:\n  listen: bad\n", "server:\n  listen: ':70000'\n",
		"server:\n  cache_expire_sec: -1\n", "server:\n  cache_expire_sec: 9223372036854775807\n",
		"server:\n  db_path: ''\n", "server:\n  access_token: a\n  access_token: b\n",
		"subscription:\n  filename: private-secret\n---\nserver: {}\n", "subscription:\n  script: ./missing.js\n",
		"subscription:\n  template: ftp://example.org/file\n", "subscription:\n  sources:\n    - url: '" + testVLESSBase + "'\n",
		"server:\n  access_token: a\nsubscription:\n  sources:\n    - url: vmess://private-secret\n",
		"server:\n  access_token: a\nsubscription:\n  sources:\n    - url: '" + testVLESSBase + "?type=xhttp'\n",
		"server:\n  cache_expire_sec: private-secret\n",
	} {
		filename := writeTestConfig(t, t.TempDir(), body)
		_, err := loadAppConfig(filename)
		if err == nil {
			t.Fatalf("expected invalid configuration to fail: %s", body)
		}
		if strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("configuration error leaked scalar value: %s", err)
		}
	}
	if _, err := loadAppConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("explicit missing config must fail")
	}
	t.Setenv("CACHE_EXPIRE_SEC", "bad")
	if _, err := loadAppConfig(""); err == nil {
		t.Fatal("invalid environment value must fail")
	}
}

func TestExampleServerConfigLoads(t *testing.T) {
	clearConfigEnv(t)
	if _, err := loadAppConfig("config.example.yaml"); err != nil {
		t.Fatal(err)
	}
}
