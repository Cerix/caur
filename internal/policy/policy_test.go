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
		{"clean without findings", review.Result{Verdict: "clean"}, false},
		{"clean with only info/low", review.Result{Verdict: "clean", Findings: []review.Finding{f("info"), f("low")}}, false},
		{"clean with one medium", review.Result{Verdict: "clean", Findings: []review.Finding{f("medium")}}, true},
		{"clean with one high", review.Result{Verdict: "clean", Findings: []review.Finding{f("high")}}, true},
		{"suspicious", review.Result{Verdict: "suspicious"}, true},
		{"malicious", review.Result{Verdict: "malicious", Findings: []review.Finding{f("critical")}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Evaluate(c.res, cfg)
			if d.NeedConfirm != c.wantBlock {
				t.Errorf("NeedConfirm = %v, want %v", d.NeedConfirm, c.wantBlock)
			}
		})
	}
}
