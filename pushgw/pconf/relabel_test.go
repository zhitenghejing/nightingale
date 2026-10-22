package pconf

import (
	"testing"

	"github.com/prometheus/prometheus/prompb"
)

func TestRelabelIf(t *testing.T) {
	lset := []prompb.Label{{Name: "label", Value: "x"}, {Name: "env", Value: "prod"}}
	for _, tc := range []struct {
		expression string
		matches    bool
		invalid    bool
	}{
		{"", true, false},
		{`label="x"`, true, false},
		{`label="x|y"`, false, false},
		{`label=~"x|y"`, true, false},
		{`label=~"xx"`, false, false},
		{`{label=~"x|y",env="prod"}`, true, false},
		{`{label="x",env!="prod"}`, false, false},
		{`label!~"y|z"`, true, false},
		{`missing=""`, true, false},
		{`missing!=""`, false, false},
		{`label=~"["`, false, true},
		{`up > 0`, false, true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			cfg := &RelabelConfig{If: tc.expression}
			if got := cfg.IfMatches(lset); got != tc.matches {
				t.Fatalf("direct match = %v, want %v", got, tc.matches)
			}
			err := cfg.CompileIf()
			if (err != nil) != tc.invalid {
				t.Fatalf("CompileIf error = %v, invalid = %v", err, tc.invalid)
			}
			if !tc.invalid && cfg.IfMatches(lset) != tc.matches {
				t.Fatal("cached condition changed matching semantics")
			}
		})
	}
}
