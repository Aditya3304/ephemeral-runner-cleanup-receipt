package coordinator

import (
	"golang.org/x/net/http/httpproxy"
	"net/url"
	"strings"
	"testing"
)

func TestGitHubProxyBypassesPrivateKubernetesAPI(t *testing.T) {
	c := &Coordinator{}
	l := &Ledger{Token: strings.Repeat("a", 32), GitHub: &GitHubRun{ProxyIP: "172.19.0.1"}}
	job := c.job(l)
	c.configureGitHubJob(job, l)
	env := map[string]string{}
	for _, v := range job.Spec.Template.Spec.Containers[0].Env {
		env[v.Name] = v.Value
	}
	choose := (&httpproxy.Config{HTTPSProxy: env["https_proxy"], NoProxy: env["NO_PROXY"]}).ProxyFunc()
	for _, target := range []string{"https://172.19.0.2:6443", "https://10.96.0.1:443"} {
		u, _ := url.Parse(target)
		p, e := choose(u)
		if e != nil || p != nil {
			t.Fatalf("Kubernetes API incorrectly proxied: %s %v %v", target, p, e)
		}
	}
	u, _ := url.Parse("https://github.com")
	p, e := choose(u)
	if e != nil || p == nil || p.Host != "172.19.0.1:3128" {
		t.Fatalf("GitHub bypasses allowlist proxy: %v %v", p, e)
	}
}
