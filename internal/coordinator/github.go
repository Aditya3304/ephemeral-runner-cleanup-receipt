package coordinator

import (
	"errors"
	"net/netip"
	"regexp"
)

// GitHubRun is public provenance obtained by the operator-side GitHub adapter.
// Registration credentials are never persisted in a ledger or signed receipt.
type GitHubRun struct {
	JobID      int64  `json:"job_id"`
	RunnerID   int64  `json:"runner_id"`
	RunnerName string `json:"runner_name"`
	Workflow   string `json:"workflow"`
	ProxyIP    string `json:"proxy_ip"`
}

func (l *Ledger) ValidateIdentity() error {
	if l.Kind != "local-ci-run/v1" || !runPattern.MatchString(l.Token) || l.Identity.Validate() != nil || l.ResourceNamespace != "proof-"+l.Token || l.RunnerNamespace != "proof-runner-"+l.Token {
		return errors.New("invalid coordinator identity or resource ownership")
	}
	if l.Identity.Provider == "local" {
		if l.GitHub != nil || l.Identity.Run != l.Token {
			return errors.New("local run identity mismatch")
		}
		return nil
	}
	g := l.GitHub
	if g == nil || !regexp.MustCompile(`^[1-9][0-9]{0,19}$`).MatchString(l.Identity.Run) || g.JobID <= 0 || g.RunnerID <= 0 || !regexp.MustCompile(`^proof-gh-[0-9]+-[0-9]+$`).MatchString(g.RunnerName) || g.Workflow != ".github/workflows/cleanup-aws.yml" {
		return errors.New("GitHub run lacks an operator-bound workflow, job and ephemeral runner")
	}
	ip, e := netip.ParseAddr(g.ProxyIP)
	if e != nil || !ip.Is4() || !ip.IsPrivate() || ip.String() != g.ProxyIP {
		return errors.New("GitHub runner requires a private operator proxy")
	}
	return nil
}
