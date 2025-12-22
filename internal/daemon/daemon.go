package daemon

import (
	_ "embed"
)

//go:embed docker-compose.yml
var DockerComposeYaml string
