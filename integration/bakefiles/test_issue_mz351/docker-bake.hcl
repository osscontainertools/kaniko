target "app" {
  context    = "."
  dockerfile = "Dockerfile"
  target     = "app"
}

target "tools" {
  context    = "."
  dockerfile = "Dockerfile"
  target     = "tools"
}
