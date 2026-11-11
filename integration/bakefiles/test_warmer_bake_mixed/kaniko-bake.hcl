target "a" {
  dockerfile  = "a/Dockerfile"
  cache_from  = ["oci:/workspace/cache/a"]
}
target "b" {
  dockerfile  = "b/Dockerfile"
}
target "c" {
  dockerfile  = "c/Dockerfile"
  cache_from  = ["oci:/workspace/cache/c"]
}
