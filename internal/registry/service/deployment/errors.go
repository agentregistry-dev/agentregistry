package deployment

import "errors"

// ErrDeploymentDrift is returned by ApplyAgentDeployment and ApplyServerDeployment
// when the caller's desired env/providerConfig/preferRemote differs from a healthy
// existing deployment and force is not set. Callers should wrap this with %w so
// downstream code can detect it via errors.Is.
var ErrDeploymentDrift = errors.New("deployment config differs from existing; use force to replace")
