target "app" {
  destination = ["test_warmer_bake/app:latest"]
  dockerfile  = "app/Dockerfile"
  cache_from  = ["oci:/workspace/cache/app", "registry.example.com/app/cache"]
}

target "tools" {
  destination = ["test_warmer_bake/tools:latest"]
  dockerfile  = "tools/Dockerfile"
  cache_from  = ["oci:/workspace/cache/tools", "registry.example.com/tools/cache"]
}
