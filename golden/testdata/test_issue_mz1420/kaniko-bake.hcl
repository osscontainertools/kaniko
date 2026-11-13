target "busybox" {
  target      = "app"
  build_arg   = { VERSION = "1.2" }
  destination = ["registry.example.com/busybox:latest"]
}

target "golang" {
  target      = "app"
  build_arg   = { BASE = "golang:1.25", VERSION = "1.3" }
  destination = ["registry.example.com/golang:latest"]
}
