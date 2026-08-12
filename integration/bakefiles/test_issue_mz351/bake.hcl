target "app" {
  target      = "app"
  destination = ["test_issue_mz351:latest"]
}

target "tools" {
  target      = "tools"
  destination = ["test_issue_mz351_tools:latest"]
}
