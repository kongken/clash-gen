package subscription

import (
	"errors"
	"net/netip"
	"strings"
)

// Rule holds the draft values of an ordered routing rule.
type Rule struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Target string `json:"target"`
}

func (d Draft) mihomoRules() ([]string, error) {
	rules := make([]string, 0, len(d.Rules)+1)
	for index, rule := range d.Rules {
		entry, err := rule.mihomo()
		if err != nil {
			return nil, &RowValidationError{Kind: RuleRow, Number: index + 1, err: err}
		}
		rules = append(rules, entry)
	}
	matchTarget := d.MatchTarget
	if matchTarget == "" {
		matchTarget = "PROXY"
	}
	if !isRuleTarget(matchTarget) {
		return nil, errors.New("MATCH 去向必须是 PROXY、DIRECT 或 REJECT。")
	}
	return append(rules, "MATCH,"+matchTarget), nil
}

func (r Rule) mihomo() (string, error) {
	if !isRuleTarget(r.Target) {
		return "", errors.New("去向必须是 PROXY、DIRECT 或 REJECT。")
	}
	ruleType := r.Type
	value := strings.TrimSpace(r.Value)
	switch ruleType {
	case "DOMAIN", "DOMAIN-SUFFIX":
		if ruleType == "DOMAIN-SUFFIX" {
			value = strings.TrimPrefix(value, ".")
		}
		host, ok := normalizedHost(value)
		if _, err := netip.ParseAddr(strings.TrimSuffix(host, ".")); !ok || err == nil {
			return "", errors.New("请输入有效的域名。")
		}
		value = strings.ToLower(strings.TrimSuffix(host, "."))
	case "IP-CIDR":
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return "", errors.New("请输入有效的 IPv4 或 IPv6 网段。")
		}
		if prefix.Addr().Is6() {
			ruleType = "IP-CIDR6"
		}
		value = prefix.Masked().String()
	default:
		return "", errors.New("请选择域名、域名后缀或 IP 网段规则。")
	}
	return ruleType + "," + value + "," + r.Target, nil
}

func isRuleTarget(value string) bool {
	return value == "PROXY" || value == "DIRECT" || value == "REJECT"
}
