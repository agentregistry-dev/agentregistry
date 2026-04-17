[registry."localhost:${REGISTRY_PORT}"]
  mirrors = ["http://kind-registry:5000"]
  http = true
  insecure = true

[registry."kind-registry:5000"]
  http = true
  insecure = true
