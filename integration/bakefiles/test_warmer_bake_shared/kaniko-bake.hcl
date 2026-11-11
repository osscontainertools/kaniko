target "app" {
  destination = ["test_warmer_bake_shared/app:latest"]
  dockerfile  = "app/Dockerfile"
}

target "tools" {
  destination = ["test_warmer_bake_shared/tools:latest"]
  dockerfile  = "tools/Dockerfile"
}
