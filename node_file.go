package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

func isShareLink(raw string) bool {
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "ss://") || strings.HasPrefix(lower, "vless://")
}

func parseShareLink(raw string) (map[string]any, error) {
	switch {
	case strings.HasPrefix(strings.ToLower(raw), "ss://"):
		return parseSS(raw)
	case strings.HasPrefix(strings.ToLower(raw), "vless://"):
		return parseVLESS(raw)
	default:
		return nil, errors.New("expected an ss:// or vless:// share link")
	}
}

func (s SubscriptionSource) read(name string) (SubscriptionData, error) {
	if s.File != "" {
		return readNodeFile(s.File)
	}
	return ExtractProxies(s.URL, name)
}

// Node files are deliberately read on every request; editing the file does not
// require a restart and does not share the remote resource cache.
func readNodeFile(filename string) (SubscriptionData, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return SubscriptionData{}, errors.New("cannot read local node file; check the path and Docker volume mount")
	}
	if !utf8.Valid(data) {
		return SubscriptionData{}, errors.New("local node file must be UTF-8")
	}
	nodes := SubscriptionData{}
	for i, line := range strings.Split(strings.TrimPrefix(string(data), "\uFEFF"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		proxy, err := parseShareLink(line)
		if err != nil {
			return SubscriptionData{}, fmt.Errorf("local node file line %d: %w", i+1, err)
		}
		nodes.Proxies = append(nodes.Proxies, proxy)
	}
	if len(nodes.Proxies) == 0 {
		return SubscriptionData{}, errors.New("local node file contains no share links")
	}
	return nodes, nil
}
