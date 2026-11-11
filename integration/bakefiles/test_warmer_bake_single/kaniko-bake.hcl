target "app" {
  destination = ["test_warmer_bake_single/app:latest"]
  dockerfile  = "app/Dockerfile"
  cache_from  = ["oci:/workspace/cache/app"]
}
