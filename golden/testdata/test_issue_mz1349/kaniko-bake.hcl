target "app" {
  destination = ["example.com/app:latest"]
  cache_from  = ["example.com/app/cache/feature", "example.com/app/cache/main"]
  cache_to    = ["example.com/app/cache/feature"]
}

target "tools" {
  destination = ["example.com/tools:latest"]
  cache_from  = ["example.com/tools/cache"]
}
