from pathlib import Path
import json
root = Path(__file__).resolve().parent.parent / 'outputs/ephemeral-runner-cleanup-receipt-aws'
for path in (root/'internal/coordinator').glob('*.go'):
    if path.name.endswith('_test.go'): continue
    s=path.read_text()
    s=s.replace('c.runDir(l.Identity.Run)', 'c.runDir(l.Token)').replace('c.RequestCancel(l.Identity.Run)', 'c.RequestCancel(l.Token)')
    if path.name=='ledger.go':
        s=s.replace('type Ledger struct {', 'type Ledger struct {\n\tGitHub *GitHubRun `json:"github,omitempty"`')
        s=s.replace('l.Kind != "local-ci-run/v1" || l.Identity.Run != run || l.Token != run', 'l.ValidateIdentity() != nil || l.Token != run')
    if path.name=='observations.go':
        s=s.replace('l.Identity.Provider != "local"', 'l.ValidateIdentity() != nil')
    if path.name=='coordinator.go':
        s=s.replace('type Coordinator struct {', 'type Coordinator struct {\n\tGitHub *GitHubRun\n\tRunnerConfig string')
        s=s.replace('id.Provider = "local"\n\tid.Run = "pending"', 'if c.GitHub == nil { id.Provider = "local"; id.Run = "pending" } else if id.Provider != "github" { return nil, errors.New("GitHub runner requires GitHub identity") }')
        s=s.replace('id.Run = state.Token', 'if c.GitHub == nil { id.Run = state.Token }')
        s=s.replace('Kind: "local-ci-run/v1", Identity:', 'Kind: "local-ci-run/v1", GitHub:c.GitHub, Identity:')
        s=s.replace('if err = c.publish(l); err != nil {', 'if err = l.ValidateIdentity(); err != nil { return nil, err }; if err = c.publish(l); err != nil {',1)
    if path.name=='collector.go':
        s=s.replace('Identity                                                                                                                           proof.Identity `json:"identity"`', 'Identity proof.Identity `json:"identity"`\n\t\tGitHub *GitHubRun `json:"github,omitempty"`')
        s=s.replace('}{l.Identity, l.ClusterUID', '}{l.Identity, l.GitHub, l.ClusterUID')
        s=s.replace('return []string{"/usr/local/bin/proof-observe"', 'args := []string{"/usr/local/bin/proof-observe"')
        s=s.replace('strconv.Itoa(r.Seconds)}\n}', 'strconv.Itoa(r.Seconds)}\n\tif r.GitHub { args = append(args, "--github") }; return args\n}',1)
        s=s.replace('observer.Request{Run: l.Token,', 'observer.Request{GitHub:l.GitHub != nil, Run: l.Token,')
    path.write_text(s)
path=root/'internal/finalizer/core.go'
s=path.read_text().replace('l.Kind != "local-ci-run/v1" || l.Token != run || l.Identity.Run != run || l.Identity.Provider != "local"', 'l.ValidateIdentity() != nil || l.Token != run')
path.write_text(s)
path=root/'internal/finalizer/archived.go'
s=path.read_text().replace('l.Kind != "local-ci-run/v1" || l.Phase != "complete" || l.Token != r.Identity.Run', 'l.ValidateIdentity() != nil || l.Phase != "complete"')
path.write_text(s)
path=root/'internal/finalizer/schema.go'
s=path.read_text().replace('if r.Identity.Provider != "local" || !tokenPattern.MatchString(r.Identity.Run) || r.Binding.ResourceNamespace != "proof-"+r.Identity.Run || r.Binding.RunnerNamespace != "proof-runner-"+r.Identity.Run {', 'token := strings.TrimPrefix(r.Binding.ResourceNamespace, "proof-")\n\tif !tokenPattern.MatchString(token) || r.Binding.ResourceNamespace != "proof-"+token || r.Binding.RunnerNamespace != "proof-runner-"+token || (r.Identity.Provider == "local" && r.Identity.Run != token) || (r.Identity.Provider == "github" && !regexp.MustCompile(`^[1-9][0-9]{0,19}$`).MatchString(r.Identity.Run)) {')
path.write_text(s)
path=root/'internal/finalizer/receipt.schema.json'
schema=json.loads(path.read_text()); props=schema['$defs']['identity']['properties']
props['provider']={'enum':['local','github']}
props['run_id']={'type':'string','minLength':1,'maxLength':32,'pattern':'^([0-9a-f]{32}|[1-9][0-9]{0,19})$'}
path.write_text(json.dumps(schema,indent=2)+'\n')
path=root/'internal/coordinator/observer/contract.go'
s=path.read_text().replace('type Request struct {', 'type Request struct {\n\tGitHub bool `json:"github,omitempty"`')
path.write_text(s)
print('Separated GitHub identity from the random resource ownership token.')
