package http

import (
	"errors"
	"net/url"
	"strings"

	"github.com/kongken/clash-gen/internal/subscription"
)

func readRulesAndSettings(form url.Values, draft *subscription.Draft) error {
	kinds, values, targets := form["rule_type"], form["rule_value"], form["rule_target"]
	if len(kinds) != len(values) || len(kinds) != len(targets) {
		return errors.New("规则表单字段不完整，请刷新后重试。")
	}
	for index, kind := range kinds {
		draft.Rules = append(draft.Rules, subscription.Rule{Type: kind, Value: values[index], Target: targets[index]})
	}

	draft.MatchTarget = form.Get("match_target")
	if draft.MatchTarget == "PROXY" {
		draft.MatchTarget = ""
	}
	draft.Settings = subscription.Settings{
		MixedPort:    form.Get("mixed_port"),
		LogLevel:     form.Get("log_level"),
		DNSEnabled:   form.Get("dns_enabled") == "1",
		DNSUpstreams: strings.ReplaceAll(form.Get("dns_upstreams"), "\r\n", "\n"),
	}
	return nil
}
