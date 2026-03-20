package openshell

import (
	"log/slog"
	"strings"

	pb "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/openshell/proto/gen"
)

// kagentADKNetworkBinaryAllowlist is the set of process paths allowed to use the
// model-provider egress rules (kagent-adk + venv python).
//
// The venv's python3 is usually a symlink to a standalone CPython under /python/
// (python-build-standalone style: cpython-<ver>-linux-<arch>-gnu). OpenShell matches the
// resolved realpath, so we list common arch triples for the same Python micro version.
// Extra entries are harmless; missing entries cause L7 proxy denials for that image.
// Bump the cpython-* paths when the kagent-adk base image changes Python version.
func kagentADKNetworkBinaryAllowlist() []*pb.NetworkBinary {
	const pyVer = "3.13.12"
	return []*pb.NetworkBinary{
		{Path: "/.kagent/.venv/bin/kagent-adk"},
		{Path: "/.kagent/.venv/bin/python3"},
		{Path: "/python/cpython-" + pyVer + "-linux-aarch64-gnu/bin/python3.13"},
		{Path: "/python/cpython-" + pyVer + "-linux-x86_64-gnu/bin/python3.13"},
	}
}

func kagentRESTEndpoint(host string, rules []*pb.L7Rule) *pb.NetworkEndpoint {
	return &pb.NetworkEndpoint{
		Host:        host,
		Port:        443,
		Protocol:    "rest",
		Tls:         "terminate",
		Enforcement: "enforce",
		Rules:       rules,
	}
}

// POST /v1/** — OpenAI-compatible and many REST inference APIs.
var kagentL7PostV1Star = []*pb.L7Rule{
	{Allow: &pb.L7Allow{Method: "POST", Path: "/v1/**"}},
}

// Google Generative Language API (Gemini).
var kagentL7GeminiPaths = []*pb.L7Rule{
	{Allow: &pb.L7Allow{Method: "POST", Path: "/v1beta/**"}},
	{Allow: &pb.L7Allow{Method: "POST", Path: "/v1/**"}},
}

// kagentNetworkPoliciesForModelProvider returns OpenShell network_policies entries for
// the agent manifest's model provider slug (MODEL_PROVIDER). Keys are stable policy map keys.
// Supported slugs align with openshellProviderMapping in deployment_adapter.go where applicable.
//
// Empty modelProvider defaults to Google Gemini API rules (previous single-policy behavior).
// Unknown slugs log a warning and return no egress rules so policy failures are visible;
// extend the switch or use a registered provider slug from openshellProviderMapping.
func kagentNetworkPoliciesForModelProvider(modelProvider string) map[string]*pb.NetworkPolicyRule {
	p := strings.ToLower(strings.TrimSpace(modelProvider))
	if p == "" {
		slog.Info("openshell: MODEL_PROVIDER empty; using Gemini (Google AI) API network policy")
		p = "google"
	}

	bin := kagentADKNetworkBinaryAllowlist()

	switch p {
	case "google", "gemini":
		return map[string]*pb.NetworkPolicyRule{
			"gemini_api": {
				Name:      "gemini-api",
				Endpoints: []*pb.NetworkEndpoint{kagentRESTEndpoint("generativelanguage.googleapis.com", kagentL7GeminiPaths)},
				Binaries:  bin,
			},
		}

	case "anthropic":
		return map[string]*pb.NetworkPolicyRule{
			"anthropic_api": {
				Name:      "anthropic-api",
				Endpoints: []*pb.NetworkEndpoint{kagentRESTEndpoint("api.anthropic.com", kagentL7PostV1Star)},
				Binaries:  bin,
			},
		}

	case "openai":
		return map[string]*pb.NetworkPolicyRule{
			"openai_api": {
				Name:      "openai-api",
				Endpoints: []*pb.NetworkEndpoint{kagentRESTEndpoint("api.openai.com", kagentL7PostV1Star)},
				Binaries:  bin,
			},
		}

	case "nvidia":
		return map[string]*pb.NetworkPolicyRule{
			"nvidia_api": {
				Name:      "nvidia-api",
				Endpoints: []*pb.NetworkEndpoint{kagentRESTEndpoint("integrate.api.nvidia.com", kagentL7PostV1Star)},
				Binaries:  bin,
			},
		}

	default:
		slog.Warn("openshell: no built-in network policy for model provider; sandbox may block model API egress",
			"model_provider", modelProvider,
		)
		return map[string]*pb.NetworkPolicyRule{}
	}
}
