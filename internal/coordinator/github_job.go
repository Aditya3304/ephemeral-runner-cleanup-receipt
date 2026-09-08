package coordinator

import (
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (c *Coordinator) configureGitHubJob(job *batch.Job, l *Ledger) {
	p := &job.Spec.Template.Spec
	g := &p.Containers[0]
	g.Command = []string{"/usr/local/bin/node"}
	g.Args = []string{"/opt/github/entrypoint.mjs"}
	g.Env = append(g.Env,
		core.EnvVar{Name: "https_proxy", Value: "http://" + l.GitHub.ProxyIP + ":3128"},
		core.EnvVar{Name: "http_proxy", Value: "http://" + l.GitHub.ProxyIP + ":3128"},
		core.EnvVar{Name: "ACTIONS_RUNNER_PRINT_LOG_TO_STDOUT", Value: "1"},
		core.EnvVar{Name: "ACTIONS_RUNNER_DISABLE_UPDATE", Value: "1"},
		core.EnvVar{Name: "PROOF_GITHUB_RUNNER_NAME", Value: l.GitHub.RunnerName},
	)
	g.Resources.Requests[core.ResourceMemory] = resource.MustParse("512Mi")
	g.Resources.Limits[core.ResourceMemory] = resource.MustParse("1536Mi")
	g.Resources.Limits[core.ResourceEphemeralStorage] = resource.MustParse("3Gi")
	g.VolumeMounts = append(g.VolumeMounts, core.VolumeMount{Name: "github-jit", MountPath: "/run/github-jit", ReadOnly: true})
	p.Volumes = append(p.Volumes, core.Volume{Name: "github-jit", VolumeSource: core.VolumeSource{Secret: &core.SecretVolumeSource{SecretName: "github-jit", DefaultMode: ptr(int32(0440))}}})
	for i := range p.Volumes {
		if p.Volumes[i].Name == "work" {
			p.Volumes[i].EmptyDir.SizeLimit = ptr(resource.MustParse("3Gi"))
		}
	}
}
