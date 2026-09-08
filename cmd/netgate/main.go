//go:build linux

// netgate is a trusted kind-node helper, never a runner executable. It enters
// only a CRI-verified, pinned pod network namespace; it never enters its mounts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const runtimeEndpoint = "unix:///run/containerd/containerd.sock"

var pending = errors.New("sandbox or network-gate init container is not ready")
var sandboxPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var uidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var namespacePattern = regexp.MustCompile(`^proof-runner-[0-9a-f]{32}$`)

type options struct {
	Sandbox, UID, Namespace, API, Endpoint string
	Proxy                                  string
}
type metadata struct{ Name, UID, Namespace string }
type inspection struct {
	Status struct {
		ID, State string
		Metadata  metadata
		Linux     struct {
			Namespaces struct {
				Options struct{ Network json.RawMessage }
			}
		}
	}
	Info struct {
		PID int `json:"pid"`
	}
}

func (o options) validate() error {
	if !sandboxPattern.MatchString(o.Sandbox) || !uidPattern.MatchString(o.UID) || !namespacePattern.MatchString(o.Namespace) {
		return errors.New("invalid sandbox, pod UID, or runner namespace")
	}
	for _, value := range []string{o.API, o.Endpoint} {
		ip, err := netip.ParseAddr(value)
		if err != nil || !ip.Is4() || !ip.IsPrivate() || ip.String() != value {
			return errors.New("API addresses must be canonical private IPv4 literals")
		}
	}
	if o.Proxy != "" {
		ip, err := netip.ParseAddr(o.Proxy)
		if err != nil || !ip.Is4() || !ip.IsPrivate() || ip.String() != o.Proxy {
			return errors.New("proxy must be a canonical private IPv4 literal")
		}
	}
	return nil
}

func trustedCommand(ctx context.Context, fd *os.File, binary string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	if fd != nil {
		cmd.ExtraFiles = []*os.File{fd}
	}
	return cmd
}

func command(ctx context.Context, fd *os.File, binary string, args ...string) ([]byte, error) {
	cmd := trustedCommand(ctx, fd, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %.2048s", binary, err, out)
	}
	if len(out) > 8<<20 {
		return nil, errors.New("trusted command output exceeds limit")
	}
	return out, nil
}

func inspect(ctx context.Context, o options) (inspection, error) {
	var result inspection
	out, err := command(ctx, nil, "/usr/local/bin/crictl", "--runtime-endpoint", runtimeEndpoint, "inspectp", o.Sandbox)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return result, pending
		}
		return result, err
	}
	return checkInspection(out, o)
}

func checkInspection(out []byte, o options) (inspection, error) {
	var result inspection
	if err := json.Unmarshal(out, &result); err != nil {
		return result, err
	}
	if result.Status.ID != o.Sandbox || result.Status.Metadata.UID != o.UID || result.Status.Metadata.Namespace != o.Namespace {
		return result, errors.New("CRI sandbox identity mismatch")
	}
	if result.Status.State != "SANDBOX_READY" || result.Info.PID <= 1 {
		return result, pending
	}
	// Reject host network mode even before comparing the actual namespace inode.
	network := string(result.Status.Linux.Namespaces.Options.Network)
	if network != `"POD"` && network != "0" {
		return result, errors.New("CRI sandbox does not use pod networking")
	}
	return result, nil
}

func verifyPinned(ctx context.Context, o options, fd *os.File, pid int) error {
	s, err := inspect(ctx, o)
	if err != nil {
		return err
	}
	if s.Info.PID != pid {
		return errors.New("sandbox PID changed")
	}
	pinned, err := fd.Stat()
	if err != nil {
		return err
	}
	current, err := os.Stat(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return pending
	}
	host, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		return err
	}
	if os.SameFile(pinned, host) || !os.SameFile(pinned, current) {
		return errors.New("host namespace or changed sandbox network namespace refused")
	}
	return nil
}

func initWaiting(ctx context.Context, o options) error {
	out, err := command(ctx, nil, "/usr/local/bin/crictl", "--runtime-endpoint", runtimeEndpoint, "ps", "--pod", o.Sandbox, "-o", "json")
	if err != nil {
		return err
	}
	var result struct {
		Containers []struct {
			PodSandboxID, State string
			Metadata            metadata
			Labels              map[string]string
		}
	}
	if err = json.Unmarshal(out, &result); err != nil {
		return err
	}
	waiting := false
	for _, c := range result.Containers {
		if c.State != "CONTAINER_RUNNING" {
			continue
		}
		if c.PodSandboxID != o.Sandbox || c.Metadata.Name != "network-gate" || c.Labels["io.kubernetes.pod.uid"] != o.UID {
			return errors.New("refusing partial firewall installation with a workload already running")
		}
		waiting = true
	}
	if !waiting {
		return pending
	}
	return nil
}

type table map[string][]string
type firewall struct {
	binary   string
	expected table
}

func model(o options) []firewall {
	// Spell out defaults because iptables -S prints the reject type explicitly.
	reject4 := "-j REJECT --reject-with icmp-port-unreachable"
	reject6 := "-j REJECT --reject-with icmp6-port-unreachable"
	input4 := []string{"-i lo -j ACCEPT", "-m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT", reject4}
	input6 := []string{"-i lo -j ACCEPT", "-m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT", reject6}
	egress := []string{"-o lo -j ACCEPT", "-d " + o.API + "/32 -p tcp -m tcp --dport 443 -j ACCEPT", "-d " + o.Endpoint + "/32 -p tcp -m tcp --dport 6443 -j ACCEPT"}
	if o.Proxy != "" {
		egress = append(egress, "-d "+o.Proxy+"/32 -p tcp -m tcp --dport 3128 -j ACCEPT")
	}
	egress = append(egress, reject4)
	return []firewall{
		{"/usr/sbin/iptables", table{
			"PROOF-EGRESS":  egress,
			"PROOF-INGRESS": input4,
		}},
		{"/usr/sbin/ip6tables", table{"PROOF-EGRESS": {"-o lo -j ACCEPT", reject6}, "PROOF-INGRESS": input6}},
	}
}

func namespaceArgs(binary string, args ...string) []string {
	argv := []string{"--net=/proc/self/fd/3", "--", binary, "-w", "5", "-t", "filter"}
	return append(argv, args...)
}

func namespaceCommand(ctx context.Context, fd *os.File, binary string, args ...string) ([]byte, error) {
	return command(ctx, fd, "/usr/bin/nsenter", namespaceArgs(binary, args...)...)
}

func parseTable(out []byte) (table, error) {
	t := table{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return nil, errors.New("invalid iptables listing")
		}
		switch parts[0] {
		case "-N", "-P":
			if _, exists := t[parts[1]]; exists {
				return nil, errors.New("duplicate chain")
			}
			t[parts[1]] = []string{}
		case "-A":
			if _, exists := t[parts[1]]; !exists {
				return nil, errors.New("rule before chain")
			}
			t[parts[1]] = append(t[parts[1]], strings.Join(parts[2:], " "))
		default:
			return nil, errors.New("unexpected iptables listing")
		}
	}
	return t, nil
}

func readTable(ctx context.Context, fd *os.File, fw firewall) (table, error) {
	out, err := namespaceCommand(ctx, fd, fw.binary, "-S")
	if err != nil {
		return nil, err
	}
	return parseTable(out)
}

// plan permits only append-only completion of an exact expected prefix. A
// completed firewall is never flushed or rebuilt, including on running pods.
func plan(actual table, fw firewall) ([][]string, error) {
	var commands [][]string
	for _, pair := range [][2]string{{"PROOF-EGRESS", "OUTPUT"}, {"PROOF-INGRESS", "INPUT"}} {
		chain, base := pair[0], pair[1]
		expected := fw.expected[chain]
		current, exists := actual[chain]
		if len(current) > len(expected) || !slices.Equal(current, expected[:len(current)]) {
			return nil, fmt.Errorf("existing %s rules differ from exact expected policy", chain)
		}
		if !exists {
			commands = append(commands, []string{"-N", chain})
		}
		for _, rule := range expected[len(current):] {
			commands = append(commands, append([]string{"-A", chain}, strings.Fields(rule)...))
		}
		baseRules, exists := actual[base]
		if !exists {
			return nil, fmt.Errorf("missing builtin chain %s", base)
		}
		jump := "-j " + chain
		found := false
		for index, rule := range baseRules {
			if strings.Contains(rule, "PROOF-") {
				if rule != jump || index != 0 || found {
					return nil, fmt.Errorf("unexpected %s gate jump", base)
				}
				found = true
			}
		}
		if !found {
			commands = append(commands, []string{"-I", base, "1", "-j", chain})
		}
	}
	return commands, nil
}

func install(ctx context.Context, o options) error {
	if err := o.validate(); err != nil {
		return err
	}
	// Serialize attempts for this UID in trusted node storage, never pod storage.
	n, err := syscall.Open("/run/proof-netgate-"+o.UID+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	lock := os.NewFile(uintptr(n), "gate-lock")
	defer lock.Close()
	for {
		err = syscall.Flock(n, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	s, err := inspect(ctx, o)
	if err != nil {
		return err
	}
	fd, err := os.Open("/proc/" + strconv.Itoa(s.Info.PID) + "/ns/net")
	if err != nil {
		return pending
	}
	defer fd.Close()
	if err = verifyPinned(ctx, o, fd, s.Info.PID); err != nil {
		return err
	}
	models := model(o)
	var plans [][][]string
	needsInstall := false
	for _, fw := range models {
		actual, err := readTable(ctx, fd, fw)
		if err != nil {
			return err
		}
		commands, err := plan(actual, fw)
		if err != nil {
			return err
		}
		plans = append(plans, commands)
		needsInstall = needsInstall || len(commands) > 0
	}
	if needsInstall {
		if err = initWaiting(ctx, o); err != nil {
			return err
		}
		for i, fw := range models {
			for _, args := range plans[i] {
				if _, err = namespaceCommand(ctx, fd, fw.binary, args...); err != nil {
					return err
				}
			}
		}
	}
	for _, fw := range models {
		actual, err := readTable(ctx, fd, fw)
		if err != nil {
			return err
		}
		remaining, err := plan(actual, fw)
		if err != nil {
			return err
		}
		if len(remaining) != 0 {
			return errors.New("firewall verification incomplete")
		}
	}
	return verifyPinned(ctx, o, fd, s.Info.PID)
}

func main() {
	var o options
	flag.StringVar(&o.Sandbox, "sandbox", "", "exact CRI sandbox ID")
	flag.StringVar(&o.UID, "pod-uid", "", "expected Kubernetes pod UID")
	flag.StringVar(&o.Namespace, "namespace", "", "owned runner namespace")
	flag.StringVar(&o.API, "api-ip", "", "local API Service IPv4 address")
	flag.StringVar(&o.Endpoint, "endpoint-ip", "", "local control-plane IPv4 address")
	flag.StringVar(&o.Proxy, "proxy-ip", "", "operator GitHub proxy IPv4 address; optional")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := install(ctx, o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, pending) {
			os.Exit(75)
		}
		os.Exit(1)
	}
	fmt.Println("verified pod network gate for " + o.UID)
}
