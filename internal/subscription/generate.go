package subscription

import (
	"errors"

	"gopkg.in/yaml.v3"
)

type mihomoConfig struct {
	Settings mihomoSettings   `yaml:",inline"`
	AllowLAN bool             `yaml:"allow-lan"`
	Mode     string           `yaml:"mode"`
	Proxies  []map[string]any `yaml:"proxies"`
	Groups   []proxyGroup     `yaml:"proxy-groups"`
	Rules    []string         `yaml:"rules"`
}

type proxyGroup struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Proxies []string `yaml:"proxies"`
}

// GenerateYAML validates the draft and produces a complete Mihomo configuration.
func GenerateYAML(draft Draft) ([]byte, error) {
	if len(draft.Nodes) == 0 {
		return nil, errors.New("请至少添加一个有效节点。")
	}
	proxies := make([]map[string]any, 0, len(draft.Nodes))
	names := make([]string, 0, len(draft.Nodes))
	seen := make(map[string]bool, len(draft.Nodes))
	for index, node := range draft.Nodes {
		proxy, err := node.proxy()
		if err != nil {
			return nil, &RowValidationError{Kind: NodeRow, Number: index + 1, err: err}
		}
		name := proxy["name"].(string)
		if seen[name] {
			return nil, &RowValidationError{Kind: NodeRow, Number: index + 1, err: errors.New("节点名称必须唯一。")}
		}
		seen[name] = true
		names = append(names, name)
		proxies = append(proxies, proxy)
	}
	settings, err := draft.Settings.mihomo()
	if err != nil {
		return nil, err
	}
	rules, err := draft.mihomoRules()
	if err != nil {
		return nil, err
	}
	output, err := yaml.Marshal(mihomoConfig{
		Settings: settings,
		Mode:     "rule",
		Proxies:  proxies,
		Groups:   []proxyGroup{{Name: "PROXY", Type: "select", Proxies: names}},
		Rules:    rules,
	})
	if err != nil {
		return nil, errors.New("无法生成 Mihomo YAML。")
	}
	return output, nil
}
