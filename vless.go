package main

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var vlessUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// parseVLESS converts share links to Mihomo proxy fields. Errors deliberately
// describe fields rather than echoing the URI or credential values.
func parseVLESS(raw string) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "vless") || u.User == nil || u.Hostname() == "" {
		return nil, errors.New("invalid VLESS share link")
	}
	if _, hasPassword := u.User.Password(); hasPassword || !vlessUUIDPattern.MatchString(u.User.Username()) {
		return nil, errors.New("VLESS userinfo must be a UUID without a password")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("VLESS port must be between 1 and 65535")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("VLESS transport path must be supplied in the path query parameter")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.New("invalid VLESS query encoding; use the original share link")
	}
	for key, values := range q {
		if len(values) != 1 {
			return nil, fmt.Errorf("duplicate VLESS parameter: %s", key)
		}
	}
	take := func(key string) string {
		value := q.Get(key)
		delete(q, key)
		return value
	}
	if encryption := take("encryption"); encryption != "" && encryption != "none" {
		return nil, errors.New("only VLESS encryption=none is supported")
	}
	name := strings.TrimSpace(u.Fragment)
	if name == "" {
		name = "VLESS " + u.Host
	}
	proxy := map[string]any{
		"name": name, "type": "vless", "server": u.Hostname(),
		"port": port, "uuid": u.User.Username(), "udp": true,
	}
	network := take("type")
	if network == "" || network == "raw" {
		network = "tcp"
	}
	security := take("security")
	if security == "" {
		security = "none"
	}
	if security != "none" && security != "tls" && security != "reality" {
		return nil, errors.New("VLESS security must be none, tls or reality")
	}
	proxy["tls"] = security != "none"
	if security != "none" {
		sni, serverName := take("sni"), take("serverName")
		if sni != "" && serverName != "" && sni != serverName {
			return nil, errors.New("conflicting VLESS sni and serverName")
		}
		if sni == "" {
			sni = serverName
		}
		if sni != "" {
			proxy["servername"] = sni
		}
		fp := take("fp")
		if security == "reality" && fp == "" {
			fp = "chrome"
		}
		if fp != "" {
			proxy["client-fingerprint"] = fp
		}
		if alpn := take("alpn"); alpn != "" {
			protocols := strings.Split(alpn, ",")
			for i := range protocols {
				protocols[i] = strings.TrimSpace(protocols[i])
				if protocols[i] == "" {
					return nil, errors.New("VLESS alpn contains an empty protocol")
				}
			}
			proxy["alpn"] = protocols
		}
		for _, key := range []string{"allowInsecure", "insecure"} {
			if value := take(key); value != "" {
				insecure, err := strconv.ParseBool(value)
				if err != nil {
					return nil, fmt.Errorf("VLESS %s must be a boolean", key)
				}
				if previous, ok := proxy["skip-cert-verify"]; ok && previous != insecure {
					return nil, errors.New("conflicting VLESS allowInsecure and insecure")
				}
				proxy["skip-cert-verify"] = insecure
			}
		}
		if security == "reality" {
			pbk, sid := take("pbk"), take("sid")
			publicKey, err := base64.RawURLEncoding.DecodeString(pbk)
			if err != nil || len(publicKey) != 32 {
				return nil, errors.New("VLESS Reality pbk must be a 32-byte base64url public key")
			}
			shortID, err := hex.DecodeString(sid)
			if err != nil || len(shortID) > 8 {
				return nil, errors.New("VLESS Reality sid must be an even-length hex string of at most 16 characters")
			}
			if sni == "" {
				return nil, errors.New("VLESS Reality requires sni or serverName")
			}
			proxy["reality-opts"] = map[string]any{"public-key": pbk, "short-id": sid}
		}
	}
	if flow := take("flow"); flow != "" {
		if flow != "xtls-rprx-vision" || network != "tcp" || security == "none" {
			return nil, errors.New("VLESS flow supports xtls-rprx-vision only with TCP and TLS/Reality")
		}
		proxy["flow"] = flow
	}
	if encoding := take("packetEncoding"); encoding != "" {
		if encoding != "xudp" && encoding != "packetaddr" && encoding != "packet" {
			return nil, errors.New("unsupported VLESS packetEncoding")
		}
		proxy["packet-encoding"] = encoding
	}
	switch network {
	case "tcp":
		if header := take("headerType"); header != "" && header != "none" {
			return nil, errors.New("VLESS TCP headerType supports none only")
		}
	case "ws", "httpupgrade":
		opts := map[string]any{}
		if path := take("path"); path != "" {
			opts["path"] = path
		}
		if host := take("host"); host != "" {
			opts["headers"] = map[string]string{"Host": host}
		}
		if earlyData := take("ed"); earlyData != "" {
			n, err := strconv.Atoi(earlyData)
			if err != nil || n < 0 {
				return nil, errors.New("VLESS ed must be a nonnegative integer")
			}
			opts["max-early-data"] = n
			opts["early-data-header-name"] = "Sec-WebSocket-Protocol"
		}
		if header := take("eh"); header != "" {
			opts["early-data-header-name"] = header
		}
		if network == "httpupgrade" {
			opts["v2ray-http-upgrade"] = true
		}
		network = "ws"
		proxy["ws-opts"] = opts
	case "grpc":
		if mode := take("mode"); mode != "" && mode != "gun" {
			return nil, errors.New("VLESS gRPC mode supports gun only")
		}
		proxy["grpc-opts"] = map[string]any{"grpc-service-name": take("serviceName")}
	default:
		return nil, errors.New("supported VLESS transports: tcp, raw, ws, httpupgrade, grpc")
	}
	proxy["network"] = network
	for key := range q {
		return nil, fmt.Errorf("unsupported VLESS parameter for this transport/security: %s", key)
	}
	return proxy, nil
}
