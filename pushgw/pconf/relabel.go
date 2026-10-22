package pconf

import (
	"fmt"
	"log"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/promql/parser"
)

func parseRelabelIf(expression string) ([]*labels.Matcher, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, nil
	}
	if !strings.HasPrefix(expression, "{") {
		expression = "{" + expression + "}"
	}
	matchers, err := parser.ParseMetricSelector(expression)
	if err != nil {
		return nil, fmt.Errorf("parse label selector: %w", err)
	}
	return matchers, nil
}

// CompileIf validates and caches a condition before writers start sharing it.
func (c *RelabelConfig) CompileIf() error {
	matchers, err := parseRelabelIf(c.If)
	if err != nil {
		return err
	}
	c.ifMatchers = matchers
	return nil
}

// IfMatches uses Prometheus matching semantics, including empty values for
// absent labels. Direct callers without PreCheck parse locally without mutating
// the shared configuration; invalid conditions never execute the action.
func (c *RelabelConfig) IfMatches(lset []prompb.Label) bool {
	matchers := c.ifMatchers
	if matchers == nil {
		var err error
		matchers, err = parseRelabelIf(c.If)
		if err != nil {
			log.Print("invalid relabel if condition: ", err)
			return false
		}
	}
	for _, matcher := range matchers {
		value := ""
		for _, label := range lset {
			if label.Name == matcher.Name {
				value = label.Value
				break
			}
		}
		if !matcher.Matches(value) {
			return false
		}
	}
	return true
}
