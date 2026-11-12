target "both" {
  context     = "app"
  dockerfile  = "Dockerfile.custom"
  destination = ["registry.example.com/both:latest"]
}

target "only_context" {
  context     = "app"
  target      = "app"
  destination = ["registry.example.com/only_context:latest"]
}

target "only_dockerfile" {
  dockerfile  = "Dockerfile.custom"
  destination = ["registry.example.com/only_dockerfile:latest"]
}

target "neither" {
  destination = ["registry.example.com/neither:latest"]
}
