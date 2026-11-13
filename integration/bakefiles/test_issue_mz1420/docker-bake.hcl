target "full" {
  context    = "."
  dockerfile = "Dockerfile"
  target     = "app"
  args       = { VERSION = "1.2" }
}

target "slim" {
  context    = "."
  dockerfile = "Dockerfile"
  target     = "app"
  args       = { VERSION = "1.2", VARIANT = "slim" }
}
