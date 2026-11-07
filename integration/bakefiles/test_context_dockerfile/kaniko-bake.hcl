target "app" {
  context    = "app"
  dockerfile = "Dockerfile"
  destination = ["test_context_dockerfile_app:latest"]
}

target "only_context" {
  context     = "app"
  target      = "app"
  destination = ["test_context_dockerfile_only_context:latest"]
}

target "tools" {
  context    = "tools"
  dockerfile = "Dockerfile.tools"
  destination = ["test_context_dockerfile_tools:latest"]
}
