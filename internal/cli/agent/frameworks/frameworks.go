package frameworks

import (
	"fmt"

	adkpython "github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
)

// Generator describes a framework/language scaffold generator.
type Generator interface {
	Generate(agentConfig *common.AgentConfig) error
}

// NewGenerator instantiates the generator for the requested framework/language.
func NewGenerator(framework, language string) (Generator, error) {
	switch framework {
	case "adk":
		switch language {
		case "python":
			return adkpython.NewPythonGenerator(), nil
		default:
			return nil, fmt.Errorf("unsupported language %q for framework %q", language, framework)
		}
	default:
		return nil, fmt.Errorf("unsupported framework: %s", framework)
	}
}
