target "app" {
  context    = "app"
  dockerfile = "Dockerfile"
}

target "only_context" {
  context = "app"
  target  = "app"
}

target "tools" {
  context    = "tools"
  dockerfile = "Dockerfile.tools"
}
