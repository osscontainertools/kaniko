target "full" {
  target      = "app"
  build_arg   = { VERSION = "1.2" }
  destination = ["test_issue_mz1420_full:latest"]
}

target "slim" {
  target      = "app"
  build_arg   = { VERSION = "1.2", VARIANT = "slim" }
  destination = ["test_issue_mz1420_slim:latest"]
}
