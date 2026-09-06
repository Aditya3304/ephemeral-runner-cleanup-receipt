//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func fixtureOptions() options {
	return options{strings.Repeat("a", 64), "796b063d-c008-4432-a6aa-891b186535d2", "proof-runner-" + strings.Repeat("b", 32), "10.96.0.1", "172.19.0.2"}
}

// These are canonical -S outputs, including the defaults that caused the first
// live install to fail verification. Keep independent of model()/plan().
const canonical4 = `-P INPUT ACCEPT
-P FORWARD ACCEPT
-P OUTPUT ACCEPT
-N PROOF-EGRESS
-N PROOF-INGRESS
-A INPUT -j PROOF-INGRESS
-A OUTPUT -j PROOF-EGRESS
-A PROOF-EGRESS -o lo -j ACCEPT
-A PROOF-EGRESS -d 10.96.0.1/32 -p tcp -m tcp --dport 443 -j ACCEPT
-A PROOF-EGRESS -d 172.19.0.2/32 -p tcp -m tcp --dport 6443 -j ACCEPT
-A PROOF-EGRESS -j REJECT --reject-with icmp-port-unreachable
-A PROOF-INGRESS -i lo -j ACCEPT
-A PROOF-INGRESS -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
-A PROOF-INGRESS -j REJECT --reject-with icmp-port-unreachable
`

const canonical6 = `-P INPUT ACCEPT
-P FORWARD ACCEPT
-P OUTPUT ACCEPT
-N PROOF-EGRESS
-N PROOF-INGRESS
-A INPUT -j PROOF-INGRESS
-A OUTPUT -j PROOF-EGRESS
-A PROOF-EGRESS -o lo -j ACCEPT
-A PROOF-EGRESS -j REJECT --reject-with icmp6-port-unreachable
-A PROOF-INGRESS -i lo -j ACCEPT
-A PROOF-INGRESS -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
-A PROOF-INGRESS -j REJECT --reject-with icmp6-port-unreachable
`

func TestGateCanonicalVerificationDoesNotModifyInstalledRules(t *testing.T) {
	for i, raw := range []string{canonical4, canonical6} {
		actual, err := parseTable([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		commands, err := plan(actual, model(fixtureOptions())[i])
		if err != nil || len(commands) != 0 {
			t.Fatalf("family %d: commands=%v err=%v", i, commands, err)
		}
	}
}

func TestGatePartialRetryOnlyCompletesExpectedPrefix(t *testing.T) {
	for _, fw := range model(fixtureOptions()) {
		for prefix := 0; prefix <= len(fw.expected["PROOF-EGRESS"]); prefix++ {
			actual := table{"INPUT": {}, "OUTPUT": {}, "FORWARD": {}, "PROOF-EGRESS": slices.Clone(fw.expected["PROOF-EGRESS"][:prefix])}
			commands, err := plan(actual, fw)
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range commands {
				switch args[0] {
				case "-N":
					if _, exists := actual[args[1]]; exists {
						t.Fatal("recreated existing chain")
					}
					actual[args[1]] = []string{}
				case "-A":
					actual[args[1]] = append(actual[args[1]], strings.Join(args[2:], " "))
				case "-I":
					if args[2] != "1" {
						t.Fatal("gate must be first rule")
					}
					if !slices.Equal(actual[args[4]], fw.expected[args[4]]) {
						t.Fatal("jump installed before complete policy")
					}
					actual[args[1]] = append([]string{strings.Join(args[3:], " ")}, actual[args[1]]...)
				default:
					t.Fatalf("destructive or unexpected command: %v", args)
				}
			}
			remaining, err := plan(actual, fw)
			if err != nil || len(remaining) > 0 {
				t.Fatalf("retry not converged: %v %v", remaining, err)
			}
		}
	}
}

func TestGateTamperedRulesFailClosed(t *testing.T) {
	cases := map[string]string{
		"extra accept":      strings.Replace(canonical4, "-A PROOF-EGRESS -o lo", "-A PROOF-EGRESS -j ACCEPT\n-A PROOF-EGRESS -o lo", 1),
		"API broadened":     strings.Replace(canonical4, "10.96.0.1/32", "10.96.0.0/12", 1),
		"node port changed": strings.Replace(canonical4, "--dport 6443", "--dport 31080", 1),
		"ICMP allowed":      strings.Replace(canonical4, "-A PROOF-EGRESS -j REJECT", "-A PROOF-EGRESS -p icmp -j ACCEPT\n-A PROOF-EGRESS -j REJECT", 1),
		"jump not first":    strings.Replace(canonical4, "-A OUTPUT -j PROOF-EGRESS", "-A OUTPUT -j ACCEPT\n-A OUTPUT -j PROOF-EGRESS", 1),
		"duplicate jump":    strings.Replace(canonical4, "-A OUTPUT -j PROOF-EGRESS", "-A OUTPUT -j PROOF-EGRESS\n-A OUTPUT -j PROOF-EGRESS", 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			actual, err := parseTable([]byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = plan(actual, model(fixtureOptions())[0]); err == nil {
				t.Fatal("tampered policy accepted")
			}
		})
	}
}

func TestGateIdentityValidation(t *testing.T) {
	o := fixtureOptions()
	if err := o.validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*options){
		func(o *options) { o.Sandbox = "--help" }, func(o *options) { o.UID = "../../proc/1" },
		func(o *options) { o.Namespace = "kube-system" }, func(o *options) { o.API = "10.96.0.1;exit" },
		func(o *options) { o.API = "::ffff:10.96.0.1" }, func(o *options) { o.Endpoint = "127.0.0.1" },
		func(o *options) { o.Endpoint = "8.8.8.8" },
	} {
		bad := o
		mutate(&bad)
		if bad.validate() == nil {
			t.Fatalf("accepted invalid identity: %+v", bad)
		}
	}
}

func TestGateCRIInspectionIdentityAndPending(t *testing.T) {
	o := fixtureOptions()
	fixture := func() map[string]any {
		return map[string]any{
			"status": map[string]any{"id": o.Sandbox, "state": "SANDBOX_READY", "metadata": map[string]any{"uid": o.UID, "namespace": o.Namespace}, "linux": map[string]any{"namespaces": map[string]any{"options": map[string]any{"network": "POD"}}}}, "info": map[string]any{"pid": 1234},
		}
	}
	for _, kind := range []string{"valid", "host", "missing mode", "wrong UID", "wrong namespace", "wrong sandbox", "pending", "missing PID"} {
		t.Run(kind, func(t *testing.T) {
			data := fixture()
			status := data["status"].(map[string]any)
			md := status["metadata"].(map[string]any)
			switch kind {
			case "host":
				status["linux"] = map[string]any{"namespaces": map[string]any{"options": map[string]any{"network": "NODE"}}}
			case "missing mode":
				delete(status, "linux")
			case "wrong UID":
				md["uid"] = "other"
			case "wrong namespace":
				md["namespace"] = "kube-system"
			case "wrong sandbox":
				status["id"] = strings.Repeat("c", 64)
			case "pending":
				status["state"] = "SANDBOX_NOTREADY"
			case "missing PID":
				delete(data, "info")
			}
			raw, _ := json.Marshal(data)
			got, err := checkInspection(raw, o)
			if kind == "valid" {
				if err != nil || got.Info.PID != 1234 {
					t.Fatal(got, err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid CRI response accepted")
			}
			if (kind == "pending" || kind == "missing PID") != errors.Is(err, pending) {
				t.Fatalf("wrong retry classification: %v", err)
			}
		})
	}
}

func TestGateCommandPinsFDAndNeverEntersPodMounts(t *testing.T) {
	fd, err := os.Open("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	args := namespaceArgs("/usr/sbin/iptables", "-S")
	cmd := trustedCommand(context.Background(), fd, "/usr/bin/nsenter", args...)
	want := []string{"/usr/bin/nsenter", "--net=/proc/self/fd/3", "--", "/usr/sbin/iptables", "-w", "5", "-t", "filter", "-S"}
	if !reflect.DeepEqual(cmd.Args, want) || len(cmd.ExtraFiles) != 1 || cmd.ExtraFiles[0] != fd {
		t.Fatalf("unsafe namespace command: %v %v", cmd.Args, cmd.ExtraFiles)
	}
	for _, arg := range cmd.Args {
		if strings.Contains(arg, "--mount") || strings.Contains(arg, "--target") || arg == "sh" {
			t.Fatal("unexpected namespace/shell boundary")
		}
	}
}
