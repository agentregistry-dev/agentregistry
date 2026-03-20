package openshell

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"
)

// defaultOpenshellWorkloadContainer is the K8s container name OpenShell uses for the
// agent workload; the pod patch merges image, args, env, and securityContext into this container.
const defaultOpenshellWorkloadContainer = "sandbox"

// openshellSandboxContainerSecurityContext matches the workload container securityContext
// OpenShell applies for `openshell sandbox create --from Dockerfile` (supervisor needs
// these caps for ip netns / veth / iptables before the child drops privileges).
func openshellSandboxContainerSecurityContext() map[string]interface{} {
	return map[string]interface{}{
		"runAsUser": int64(0),
		"capabilities": map[string]interface{}{
			"add": []interface{}{
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
	containerName := defaultOpenshellWorkloadContainer
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("container image is required for pod command patch")
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("command must be non-empty")
	}
	argVals := make([]interface{}, len(command))
	for i, c := range command {
		argVals[i] = c
	}
	workload := strings.Join(command, " ")
	raw := map[string]interface{}{
		"spec": map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"name":  containerName,
					"image": image,
					// Same as CLI trailing args after `--`; supervisor prefers these over OPENSHELL_SANDBOX_COMMAND.
					"args": argVals,
					"env": []interface{}{
						map[string]interface{}{
							"name":  openshellSandboxCommandEnv,
							"value": workload,
						},
					},
					"securityContext": openshellSandboxContainerSecurityContext(),
				},
			},
		},
	}
	st, err := structpb.NewStruct(raw)
	if err != nil {
		return nil, fmt.Errorf("pod template struct: %w", err)
	}
	return st, nil
}
