# Target with both context and dockerfile
target "both" {
  context     = "app"
  dockerfile  = "Dockerfile"
  target      = "app"
  destination = ["registry.example.com/both:latest"]
}

# Target with only context set (dockerfile comes from default/CLI flag)
target "only_context" {
  context     = "app"
  target      = "app"
  destination = ["registry.example.com/only_context:latest"]
}

# Target with only dockerfile set (context comes from default/CLI flag)
target "only_dockerfile" {
  dockerfile  = "Dockerfile.custom"
  target      = "custom_df"
  destination = ["registry.example.com/only_dockerfile:latest"]
}

# Target with neither context nor dockerfile set (both come from default/CLI flags)
target "neither" {
  target      = "fallback"
  destination = ["registry.example.com/neither:latest"]
}
