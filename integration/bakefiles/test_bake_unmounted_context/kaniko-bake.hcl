# The executor image holds this folder at /ctx instead of a mount, see Dockerfile.executor.
# The filesystem cleanup between targets deletes a context that is not on a mount.
target "app" {
  context     = "app"
  destination = ["test_bake_unmounted_context_app:latest"]
}

target "tools" {
  context     = "tools"
  destination = ["test_bake_unmounted_context_tools:latest"]
}
