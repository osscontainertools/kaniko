target "app" {
  context     = "app"
  destination = ["test_bake_unmounted_context_app:latest"]
}

target "tools" {
  context     = "tools"
  destination = ["test_bake_unmounted_context_tools:latest"]
}
