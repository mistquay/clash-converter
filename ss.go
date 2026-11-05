package main

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// parseSS accepts SIP002 userinfo and legacy whole-authority Base64 links.
// Errors never include the input URI, password, or encoded credentials.
func parseSS(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "ss://") {
		return nil, errors.New("invalid SS share link")
	}
	body, fragment, _ := strings.Cut(raw[5:], "#")
	name, err := url.PathUnescape(fragment)
	if err != nil {
		return nil, errors.New("invalid SS name encoding")
	}
	body, query, _ := strings.Cut(body, "?")
	if query != "" {
		return nil, errors.New("SS query parameters (including plugins) are not supported")
	}
	legacy := !strings.Contains(body, "@")
	if legacy {
		body, err = decodeSSBase64(body)
		if err != nil {
			return nil, err
		}
	}
	at := strings.LastIndex(body, "@")
	if at < 1 {
		return nil, errors.New("invalid SS server or credentials")
	}
	credentials, address := body[:at], strings.TrimSuffix(body[at+1:], "/")
	if !legacy {
		credentials, err = url.PathUnescape(credentials)
		if err != nil {
			return nil, errors.New("invalid SS userinfo encoding")
		}
		if !strings.Contains(credentials, ":") {
			credentials, err = decodeSSBase64(credentials)
			if err != nil {
				return nil, err
			}
		}
	}
	cipher, password, ok := strings.Cut(credentials, ":")
	if !ok || cipher == "" || password == "" {
		return nil, errors.New("SS credentials must contain a cipher and password")
	}
	u, err := url.Parse("ss://" + address)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid SS server address")
	}
	_, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, errors.New("invalid SS server address or missing port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("SS port must be between 1 and 65535")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "SS " + u.Host
	}
	return map[string]any{
		"name": name, "type": "ss", "server": u.Hostname(), "port": port,
		"cipher": cipher, "password": password, "udp": true,
	}, nil
}

func decodeSSBase64(value string) (string, error) {
	for _, encoding := range []*base64.Encoding{
		base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding,
	} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return string(decoded), nil
		}
	}
	return "", errors.New("invalid SS Base64 credentials")
}
