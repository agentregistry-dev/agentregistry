package openshell

import (
	pb "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/openshell/proto/gen"
)

// kagentADKSandboxPolicy returns the default OpenShell policy for kagent-adk agents.
// modelProvider is the agent manifest MODEL_PROVIDER slug; egress rules are chosen via
// kagentNetworkPoliciesForModelProvider (see kagent_network_policies.go).
// Process identity is fixed to sandbox/sandbox to match OpenShell + kagent-adk image docs.
func kagentADKSandboxPolicy(modelProvider string) *pb.SandboxPolicy {
	return &pb.SandboxPolicy{
		Version: 1,
		Filesystem: &pb.FilesystemPolicy{
			IncludeWorkdir: true,
			ReadOnly: []string{
				"/usr", "/lib", "/proc", "/dev/urandom", "/app", "/etc", "/var/log",
			},
			ReadWrite: []string{"/sandbox", "/tmp", "/dev/null"},
		},
		Landlock: &pb.LandlockPolicy{Compatibility: "best_effort"},
		Process: &pb.ProcessPolicy{
			RunAsUser:  "sandbox",
			RunAsGroup: "sandbox",
		},
		NetworkPolicies: kagentNetworkPoliciesForModelProvider(modelProvider),
	}
}
