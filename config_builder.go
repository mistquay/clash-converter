package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SubInfoGroupName 常量定义
const (
	SubInfoGroupName = "📊 订阅信息" // 用量信息节点组名称
)

// DummyNodeConfig 假节点配置：用于显示订阅用量信息
var DummyNodeConfig = map[string]any{
	"type":     "ss",
	"server":   "127.0.0.1",
	"port":     1080,
	"cipher":   "aes-128-gcm",
	"password": "dummy",
}

// Set 集合类型
type Set map[string]bool

func NewSet(e ...string) (result Set) {
	result = make(Set, len(e))
	for _, v := range e {
		result[v] = true
	}
	return
}

func (s Set) Has(e string) bool {
	_, ok := s[e]
	return ok
}

// RuleTypes Clash 规则类型集合
var RuleTypes = NewSet(
	"DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "DOMAIN-REGEX", "GEOSITE",
	"IP-CIDR", "IP-CIDR6", "IP-SUFFIX", "IP-ASN", "GEOIP", "SRC-GEOIP", "SRC-IP-ASN",
	"SRC-IP-CIDR", "SRC-IP-SUFFIX", "DST-PORT", "SRC-PORT", "IN-PORT", "IN-TYPE",
	"IN-USER", "IN-NAME", "PROCESS-PATH", "PROCESS-PATH", "PROCESS-PATH-REGEX",
	"PROCESS-PATH-REGEX", "PROCESS-NAME", "PROCESS-NAME", "PROCESS-NAME", "PROCESS-NAME-REGEX",
	"PROCESS-NAME-REGEX", "PROCESS-NAME-REGEX", "UID", "NETWORK", "DSCP", "RULE-SET", "AND",
	"OR", "NOT", "SUB-RULE",
)

// ClashHeaders 需要透传的 Clash 特定头
var ClashHeaders = NewSet(
	"Content-Disposition", "Profile-Update-Interval",
	"Subscription-Userinfo", "Profile-Web-Page-Url",
)

// BuildTemplate 根据模板、节点和规则构建最终配置
// 规则重写逻辑：为每条规则添加 tag，支持3段和2段规则格式
func BuildTemplate(
	template string, Proxies SubscriptionData, ruleLines []*Ruleset,
) (result map[string]any, err error) {
	err = yaml.Unmarshal([]byte(template), &result)
	if err != nil {
		return
	}

	result["proxies"] = Proxies.Proxies
	rules := make([]string, 0, 4096)

	for _, rule := range ruleLines {
		tag := rule.tag
		for _, r := range strings.Split(rule.content, "\n") {
			r = strings.TrimSpace(r)
			if len(r) == 0 || r[0] == '#' {
				continue
			}

			if rule.behavior != "" && rule.behavior != "classical" {
				r, err = normalizeRule(r, rule.behavior)
				if err != nil {
					err = fmt.Errorf("invalid %s rule from %s: %w", rule.behavior, rule.url, err)
					return
				}
			}

			ruleComponents := strings.Split(r, ",")
			if len(ruleComponents) < 2 {
				err = fmt.Errorf("rules must have at least 2 componets: %s", rule.content)
				return
			}

			if !RuleTypes.Has(ruleComponents[0]) {
				continue
			}

			// 3段规则：TYPE,VALUE,OPTIONS -> TYPE,VALUE,TAG,OPTIONS
			if len(ruleComponents) == 3 {
				rules = append(rules, fmt.Sprintf(
					"%s,%s,%s,%s",
					ruleComponents[0], ruleComponents[1], tag, ruleComponents[2],
				))
			} else {
				// 2段规则：TYPE,VALUE -> TYPE,VALUE,TAG
				rules = append(rules, r+","+tag)
			}
		}
	}

	result["rules"] = rules

	return
}

func normalizeRule(rule string, behavior string) (string, error) {
	switch behavior {
	case "domain":
		if strings.HasPrefix(rule, "+.") {
			return "DOMAIN-SUFFIX," + strings.TrimPrefix(rule, "+."), nil
		}
		return "DOMAIN," + rule, nil
	case "ipcidr":
		ip, _, err := net.ParseCIDR(rule)
		if err != nil {
			return "", fmt.Errorf("%q is not a CIDR", rule)
		}
		if ip.To4() == nil {
			return "IP-CIDR6," + rule, nil
		}
		return "IP-CIDR," + rule, nil
	default:
		return "", fmt.Errorf("unsupported behavior %q", behavior)
	}
}

// Marshal 将配置序列化为 YAML 字符串
func Marshal(y map[string]any) (result string, err error) {
	resultBytes, err := yaml.Marshal(y)
	if err != nil {
		return
	}

	result = string(resultBytes)

	return
}

// addSubInfoGroup 在配置中添加用量信息节点和节点组
// 为每个订阅创建假节点显示用量，并将这些节点组成订阅信息组插入到最前面
func addSubInfoGroup(yamlStr string, subInfos []*SubscriptionMeta) (string, error) {
	if len(subInfos) == 0 {
		return yamlStr, nil
	}
	var config map[string]any
	err := yaml.Unmarshal([]byte(yamlStr), &config)
	if err != nil {
		return "", err
	}

	proxies, ok := config["proxies"].([]any)
	if !ok {
		proxies = make([]any, 0)
	}

	// 为每个订阅创建用量显示节点
	infoNodeNames := make([]string, 0, len(subInfos))
	for _, info := range subInfos {
		used := float64(info.Upload+info.Download) / 1024 / 1024 / 1024
		total := float64(info.Total) / 1024 / 1024 / 1024
		remaining := max(total-used, 0)
		nodeName := fmt.Sprintf("%s | 已用 %.1f GB / %.1f GB | 剩余 %.1f GB", info.Name, used, total, remaining)
		if info.Total == 0 {
			nodeName = info.Name + " | 流量信息不可用"
		}
		if info.Expire > 0 {
			nodeName += " | 到期 " + time.Unix(info.Expire, 0).Format("2006-01-02")
		}
		infoNodeNames = append(infoNodeNames, nodeName)

		// 创建假节点
		dummyNode := make(map[string]any)
		for k, v := range DummyNodeConfig {
			dummyNode[k] = v
		}
		dummyNode["name"] = nodeName

		proxies = append([]any{dummyNode}, proxies...)
	}

	config["proxies"] = proxies

	// 创建用量信息节点组
	subInfoGroup := map[string]any{
		"name":    SubInfoGroupName,
		"type":    "select",
		"proxies": infoNodeNames,
	}

	// 插入到节点组最前面
	proxyGroups, ok := config["proxy-groups"].([]any)
	if ok {
		config["proxy-groups"] = append([]any{subInfoGroup}, proxyGroups...)
	} else {
		config["proxy-groups"] = []any{subInfoGroup}
	}

	resultBytes, err := yaml.Marshal(config)
	if err != nil {
		return "", err
	}

	return string(resultBytes), nil
}
