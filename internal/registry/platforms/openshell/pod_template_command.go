package openshell

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
	corev1 "k8s.io/api/core/v1"
)

// defaultOpenshellWorkloadContainer is the K8s container name OpenShell uses for the
// agent workload; the pod patch merges image, args, env, and securityContext into this container.
const defaultOpenshellWorkloadContainer = "sandbox"

// openshellSandboxContainerSecurityContext matches the workload container securityContext
// OpenShell applies for `openshell sandbox create --from Dockerfile` (supervisor needs
// these caps for ip netns / veth / iptables before the child drops privileges).
func openshellSandboxContainerSecurityContext() *corev1.SecurityContext {
	u := int64(0)
	return &corev1.SecurityContext{
		RunAsUser: &u,
		Capabilities: &corev1.Capabilities{
			Add: []corev1.Capability{
				"SYS_ADMIN",
				"NET_ADMIN",
				"SYS_PTRACE",
				"SYSLOG",
			},
		},
	}
}

// podTemplatePatchForCommand builds a pod_template patch for the workload container: image,
// securityContext, container args, and OPENSHELL_SANDBOX_COMMAND env.
//
// Per OpenShell supervisor behavior, workload resolution order is: (1) argv after the entrypoint
// (Kubernetes container `args` — what `openshell sandbox create … -- <cmd>` sets), (2)
// OPENSHELL_SANDBOX_COMMAND (server default "sleep infinity"), (3) /bin/bash. We set both `args`
// and the env var so gateways that merge pod specs in different orders still run kagent-adk.
//
// SandboxSpec.environment / SandboxTemplate.environment alone do not replace the server’s default
// pod env; pod_template on the workload container does.
//
// The command parameter must be non-empty so agent deploys still take this code path when BYO
// image + kagent workload are used.
//
// BYO-image CreateSandbox sandboxes may omit the same securityContext as CLI --from
// flows; without SYS_ADMIN/NET_ADMIN the supervisor fails creating the network namespace.
func podTemplatePatchForCommand(image string, command []string) (*structpb.Struct, error) {
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("container image is required for pod command patch")
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("command must be non-empty")
	}

	args := append([]string(nil), command...)
	workload := strings.Join(command, " ")

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  defaultOpenshellWorkloadContainer,
					Image: image,
					Args:  args,
					Env: []corev1.EnvVar{
						{Name: openshellSandboxCommandEnv, Value: workload},
					},
					SecurityContext: openshellSandboxContainerSecurityContext(),
				},
			},
		},
	}
	return podToStructPB(pod)
}

// podToStructPB converts a Kubernetes Pod to protobuf Struct for SandboxTemplate.pod_template.
// JSON is the stable interchange format between k8s.io/api types and structpb.
func podToStructPB(pod *corev1.Pod) (*structpb.Struct, error) {
	data, err := json.Marshal(pod)
	if err != nil {
		return nil, fmt.Errorf("marshal pod: %w", err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("unmarshal pod json: %w", err)
	}
	st, err := structpb.NewStruct(root)
	if err != nil {
		return nil, fmt.Errorf("pod template struct: %w", err)
	}
	return st, nil
}
