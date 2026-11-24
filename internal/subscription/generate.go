package subscription

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

type mihomoConfig struct {
	MixedPort int              `yaml:"mixed-port"`
	AllowLAN  bool             `yaml:"allow-lan"`
	Mode      string           `yaml:"mode"`
	LogLevel  string           `yaml:"log-level"`
	DNS       mihomoDNS        `yaml:"dns"`
	Proxies   []map[string]any `yaml:"proxies"`
	Groups    []proxyGroup     `yaml:"proxy-groups"`
	Rules     []string         `yaml:"rules"`
}

type mihomoDNS struct {
	Enable bool `yaml:"enable"`
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
			return nil, fmt.Errorf("节点 %d：%w", index+1, err)
		}
		name := proxy["name"].(string)
		if seen[name] {
			return nil, fmt.Errorf("节点 %d：节点名称必须唯一。", index+1)
		}
		seen[name] = true
		names = append(names, name)
		proxies = append(proxies, proxy)
	}
	output, err := yaml.Marshal(mihomoConfig{
		MixedPort: 7890,
		Mode:      "rule",
		LogLevel:  "info",
		Proxies:   proxies,
		Groups:    []proxyGroup{{Name: "PROXY", Type: "select", Proxies: names}},
		Rules:     []string{"MATCH,PROXY"},
	})
	if err != nil {
		return nil, errors.New("无法生成 Mihomo YAML。")
	}
	return output, nil
}
