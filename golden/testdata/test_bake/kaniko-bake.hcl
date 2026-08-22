# the image customers get
target "app" {
  target      = "app"
  destination = ["registry.example.com/app:latest", "registry.example.com/app:v1"]
}

# internal only, never promoted
target "tools" {
  target      = "tools"
  destination = ["registry.example.com/tools:latest"]
}
