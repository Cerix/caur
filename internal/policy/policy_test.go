package policy

import (
	"testing"

	"caur/internal/config"
	"caur/internal/review"
)

func TestEvaluate(t *testing.T) {
	cfg := config.Default() // block_threshold=1, auto_approve_clean=true

	f := func(sev string) review.Finding { return review.Finding{Severity: sev} }

	cases := []struct {
		name      string
		res       review.Result
		wantBlock bool
	}{
		{"clean senza findings", review.Result{Verdict: "clean"}, false},
		{"clean con soli info/low", review.Result{Verdict: "clean", Findings: []review.Finding{f("info"), f("low")}}, false},
		{"clean con un medium", review.Result{Verdict: "clean", Findings: []review.Finding{f("medium")}}, true},
		{"clean con un high", review.Result{Verdict: "clean", Findings: []review.Finding{f("high")}}, true},
		{"suspicious", review.Result{Verdict: "suspicious"}, true},
		{"malicious", review.Result{Verdict: "malicious", Findings: []review.Finding{f("critical")}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Evaluate(c.res, cfg)
			if d.NeedConfirm != c.wantBlock {
				t.Errorf("NeedConfirm = %v, atteso %v", d.NeedConfirm, c.wantBlock)
			}
		})
	}
}
