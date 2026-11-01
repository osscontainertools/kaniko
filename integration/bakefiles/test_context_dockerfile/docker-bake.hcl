target "app" {
  context    = "app"
  dockerfile = "Dockerfile"
}

target "tools" {
  context    = "tools"
  dockerfile = "Dockerfile.tools"
}
