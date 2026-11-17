target "missing" {
  context    = "app"
  dockerfile = "Missing.Dockerfile"
}

target "tools" {
  context = "tools"
}
