variable "REGISTRY" {
  default = "localhost"
}

variable "TAG" {
  default = "latest"
}

variable "IS_RELEASE" {
  default = false
}

group "default" {
  targets = ["executor"]
}

target "executor" {
  target = "kaniko-executor"
  context = "."
  dockerfile = "deploy/Dockerfile"
  tags = [
    "${REGISTRY}:${TAG}",
    IS_RELEASE ? "${REGISTRY}:latest": ""
  ]
  no-cache-filter = ["certs"]
  cache-from = ["type=gha"]
  cache-to   = ["type=gha,mode=max"]
}

target "debug" {
  target = "kaniko-debug"
  context = "."
  dockerfile = "deploy/Dockerfile"
  tags = [
    "${REGISTRY}:${TAG}-debug",
    IS_RELEASE ? "${REGISTRY}:debug": ""
  ]
  no-cache-filter = ["certs"]
  cache-from = ["type=gha"]
  cache-to   = ["type=gha,mode=max"]
}

target "slim" {
  target = "kaniko-slim"
  context = "."
  dockerfile = "deploy/Dockerfile"
  tags = [
    "${REGISTRY}:${TAG}-slim",
    IS_RELEASE ? "${REGISTRY}:slim": ""
  ]
  no-cache-filter = ["certs"]
  cache-from = ["type=gha"]
  cache-to   = ["type=gha,mode=max"]
}

target "warmer" {
  target = "kaniko-warmer"
  context = "."
  dockerfile = "deploy/Dockerfile"
  tags = [
    "${REGISTRY}:${TAG}-warmer",
    IS_RELEASE ? "${REGISTRY}:warmer": ""
  ]
  no-cache-filter = ["certs"]
  cache-from = ["type=gha"]
  cache-to   = ["type=gha,mode=max"]
}

target "bootstrap" {
  target = "kaniko-debug-2"
  context = "."
  dockerfile = "deploy/Dockerfile"
  tags = [
    "${REGISTRY}:${TAG}-bootstrap",
    IS_RELEASE ? "${REGISTRY}:bootstrap": ""
  ]
  no-cache-filter = ["certs"]
  cache-from = ["type=gha"]
  cache-to   = ["type=gha,mode=max"]
}
