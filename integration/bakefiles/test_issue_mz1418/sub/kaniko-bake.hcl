target "app" {
  context     = "app"
  destination = ["test_issue_mz1418_app:latest"]
}

target "tools" {
  dockerfile  = "tools/Dockerfile"
  destination = ["test_issue_mz1418_tools:latest"]
}
