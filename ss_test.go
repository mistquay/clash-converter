package main

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

const testSSBase = "ss://" + "cmM0LW1kNTp0ZXN0LXBhc3N3b3Jk" + "@ss.example.org:6000"

func TestParseSSFormats(t *testing.T) {
	password := "test:password/?+@雪"
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		for _, legacy := range []bool{false, true} {
			authority := encoding.EncodeToString([]byte("rc4-md5:"+password)) + "@ss.example.org:80/"
			if legacy {
				authority = encoding.EncodeToString([]byte("rc4-md5:" + password + "@ss.example.org:80"))
			}
			proxy, err := parseSS("ss://" + authority + "#" + url.PathEscape("腾讯云 广州"))
			if err != nil {
				t.Fatal(err)
			}
			if proxy["name"] != "腾讯云 广州" || proxy["server"] != "ss.example.org" || proxy["port"] != 80 ||
				proxy["password"] != password || proxy["cipher"] != "rc4-md5" || proxy["udp"] != true {
				t.Fatalf("SS fields were not preserved: %#v", proxy)
			}
		}
	}
	proxy, err := parseSS("SS://aes-128-gcm:" + url.PathEscape(password) + "@[2001:db8::1]:443")
	if err != nil {
		t.Fatal(err)
	}
	if proxy["server"] != "2001:db8::1" || proxy["name"] != "SS [2001:db8::1]:443" || proxy["password"] != password {
		t.Fatalf("IPv6 or plaintext userinfo failed: %#v", proxy)
	}
}

func TestParseSSRejectsMalformedLinksWithoutCredentials(t *testing.T) {
	for _, raw := range []string{
		"http://private-secret", "ss://private-secret", "ss://%%%private-secret@host:80",
		"ss://rc4-md5:@host:80#private-secret", "ss://:private-secret@host:80",
		"ss://rc4-md5:private-secret@host", "ss://rc4-md5:private-secret@host:0",
		"ss://rc4-md5:private-secret@host:65536", "ss://rc4-md5:private-secret@host:abc",
		"ss://rc4-md5:private-secret@host:80:90", "ss://rc4-md5:private-secret@:80",
		"ss://rc4-md5:private-secret@host:80/path", "ss://rc4-md5:private-secret@host:80#%zz",
		"ss://rc4-md5:private-secret@host:80?plugin=obfs-local",
		"ss://" + base64.RawStdEncoding.EncodeToString([]byte("rc4-md5:private-secret@host:80?plugin=obfs")),
	} {
		if _, err := parseSS(raw); err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("expected a credential-free error for malformed SS link, got %v", err)
		}
	}
}

func TestSSSourceHasNoSubscriptionStatistics(t *testing.T) {
	if err := validateSource(testSSBase); err != nil {
		t.Fatal(err)
	}
	nodes, err := ExtractProxies(testSSBase, "unused")
	if err != nil || len(nodes.Proxies) != 1 || len(nodes.SubInfos) != 0 || len(nodes.TransparentHeaders) != 0 {
		t.Fatalf("SS source must only contain a proxy: %#v, %v", nodes, err)
	}
}
