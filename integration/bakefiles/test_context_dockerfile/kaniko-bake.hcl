target "app" {
  context    = "app"
  dockerfile = "Dockerfile"
  destination = ["test_context_dockerfile_app:latest"]
}

target "tools" {
  context    = "tools"
  dockerfile = "Dockerfile.tools"
  destination = ["test_context_dockerfile_tools:latest"]
}
