target "both" {
  context     = "app"
  dockerfile  = "Dockerfile"
  target      = "app"
  destination = ["registry.example.com/both:latest"]
}

target "only_context" {
  context     = "app"
  target      = "app"
  destination = ["registry.example.com/only_context:latest"]
}

target "only_dockerfile" {
  dockerfile  = "Dockerfile.custom"
  target      = "custom_df"
  destination = ["registry.example.com/only_dockerfile:latest"]
}

target "neither" {
  target      = "fallback"
  destination = ["registry.example.com/neither:latest"]
}
