package main

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const testVLESSBase = "vless://00000000-0000-4000-8000-000000000001@node.example.com:443"
const testRealityKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestParseVLESSMappings(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want map[string]any
	}{
		{"tcp", testVLESSBase + "?encryption=none&type=tcp#%E8%87%AA%E5%BB%BA", map[string]any{
			"name": "自建", "type": "vless", "server": "node.example.com", "port": 443,
			"uuid": "00000000-0000-4000-8000-000000000001", "network": "tcp", "tls": false, "udp": true,
		}},
		{"reality", testVLESSBase + "?security=reality&type=tcp&sni=www.example.org&pbk=" + testRealityKey + "&sid=0123abcd&flow=xtls-rprx-vision", map[string]any{
			"tls": true, "servername": "www.example.org", "flow": "xtls-rprx-vision", "client-fingerprint": "chrome",
			"reality-opts": map[string]any{"public-key": testRealityKey, "short-id": "0123abcd"},
		}},
		{"ws", testVLESSBase + "?security=tls&type=ws&sni=tls.example.org&fp=firefox&alpn=h2%2Chttp%2F1.1&host=ws.example.org&path=%2Fws%3Ffoo%3Da%252Fb&allowInsecure=0&ed=2048&eh=Sec-WebSocket-Protocol", map[string]any{
			"network": "ws", "tls": true, "servername": "tls.example.org", "client-fingerprint": "firefox", "skip-cert-verify": false,
			"alpn":    []string{"h2", "http/1.1"},
			"ws-opts": map[string]any{"path": "/ws?foo=a%2Fb", "headers": map[string]string{"Host": "ws.example.org"}, "max-early-data": 2048, "early-data-header-name": "Sec-WebSocket-Protocol"},
		}},
		{"grpc", testVLESSBase + "?security=tls&type=grpc&serviceName=my%2Fservice&mode=gun&serverName=grpc.example.org&packetEncoding=xudp", map[string]any{
			"network": "grpc", "servername": "grpc.example.org", "packet-encoding": "xudp", "grpc-opts": map[string]any{"grpc-service-name": "my/service"},
		}},
		{"httpupgrade", testVLESSBase + "?type=httpupgrade&path=%2Fupgrade", map[string]any{
			"network": "ws", "ws-opts": map[string]any{"path": "/upgrade", "v2ray-http-upgrade": true},
		}},
		{"ipv6", "VLESS://00000000-0000-4000-8000-000000000001@[2001:db8::1]:8443?type=raw", map[string]any{
			"server": "2001:db8::1", "port": 8443, "network": "tcp", "name": "VLESS [2001:db8::1]:8443",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy, err := parseVLESS(tt.uri)
			if err != nil {
				t.Fatal(err)
			}
			for key, want := range tt.want {
				if !reflect.DeepEqual(proxy[key], want) {
					t.Errorf("%s: got %#v, want %#v", key, proxy[key], want)
				}
			}
		})
	}
}

func TestParseVLESSRejectsUnsupportedOrInvalidLinks(t *testing.T) {
	queries := []string{
		"type=xhttp", "type=kcp", "type=tcp&headerType=http", "encryption=mlkem768x25519plus",
		"security=xtls", "security=reality&sni=example.org", "security=reality&sni=example.org&pbk=" + testRealityKey + "&sid=abc",
		"security=reality&pbk=" + testRealityKey, "security=tls&allowInsecure=maybe", "security=tls&allowInsecure=1&insecure=0",
		"security=tls&sni=a&serverName=b", "security=tls&alpn=h2%2C", "flow=xtls-rprx-vision", "security=tls&type=ws&flow=xtls-rprx-vision",
		"security=tls&flow=unsupported", "type=grpc&mode=multi", "type=ws&serviceName=ignored", "type=ws&ed=-1",
		"type=tcp&type=ws", "path=%GG", "security=tls&unknown=private-value", "packetEncoding=unknown", "spx=%2F",
	}
	for _, query := range queries {
		t.Run(query, func(t *testing.T) {
			_, err := parseVLESS(testVLESSBase + "?" + query)
			if err == nil {
				t.Fatal("expected unsupported or invalid link to be rejected")
			}
			for _, secret := range []string{testVLESSBase, "00000000-0000-4000-8000-000000000001", "node.example.com", "private-value"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error disclosed URI credentials: %s", err)
				}
			}
		})
	}
	for _, raw := range []string{
		"vless://", "vless://bad-uuid@example.org:443", "vless://uuid:password@example.org:443",
		strings.Replace(testVLESSBase, ":443", ":0", 1), strings.Replace(testVLESSBase, ":443", ":65536", 1),
		strings.Replace(testVLESSBase, ":443", "", 1), testVLESSBase + "/ignored-path", "vmess://something",
	} {
		if _, err := parseVLESS(raw); err == nil {
			t.Errorf("expected malformed URI to be rejected")
		}
	}
}

func TestVLESSSourceHasNoSubscriptionStatistics(t *testing.T) {
	nodes, err := ExtractProxies(testVLESSBase+"#"+url.PathEscape("香港节点"), "unused")
	if err != nil {
		t.Fatal(err)
	}
	setSubscriptionName(&nodes, "自建")
	merged := mergeProxies([]SubscriptionData{nodes})
	if len(merged.SubInfos) != 0 || len(merged.TransparentHeaders) != 0 || len(merged.Proxies) != 1 {
		t.Fatalf("unexpected single-node subscription data: %#v", merged)
	}
	if merged.Proxies[0]["name"] != "[自建] 香港节点" {
		t.Fatalf("unexpected node name: %v", merged.Proxies[0]["name"])
	}
}
