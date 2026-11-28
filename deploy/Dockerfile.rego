package docker

default allow := false

# Allow local build context and the Dockerfile itself
allow if input.local

# Allow scratch (nothing is pulled)
allow if {
  input.image
  input.image.ref == "scratch"
}

# Allow the scanner pulled by BuildKit for SBOM attestations
allow if {
  input.image
  input.image.ref == "docker.io/docker/buildkit-syft-scanner:stable-1"
}

# Require base images from Docker Hub, pinned by digest
allow if {
  input.image
  input.image.host == "docker.io"
  input.image.isCanonical
}

# Allow remote downloads only over HTTPS, from trusted hosts, with a checksum
allow if {
  input.http
  input.http.schema == "https"
  input.http.host in ["raw.githubusercontent.com", "github.com"]
  input.http.checksum != ""
}

deny_msg contains msg if {
  not allow
  input.image
  input.image.ref != "scratch"
  not input.image.isCanonical
  msg := sprintf("image %s must be pinned by digest (e.g., @sha256:...)", [input.image.ref])
}

deny_msg contains msg if {
  not allow
  input.image
  input.image.ref != "scratch"
  input.image.host != "docker.io"
  msg := sprintf("registry %s is not allowed; only docker.io is permitted", [input.image.host])
}

deny_msg contains msg if {
  not allow
  input.http
  input.http.schema != "https"
  msg := sprintf("download %s must use HTTPS", [input.http.url])
}

deny_msg contains msg if {
  not allow
  input.http
  not (input.http.host in ["raw.githubusercontent.com", "github.com"])
  msg := sprintf("download host %s is not allowed", [input.http.host])
}

deny_msg contains msg if {
  not allow
  input.http
  input.http.checksum == ""
  msg := sprintf("download %s must include a checksum (--checksum=sha256:...)", [input.http.url])
}

decision := {"allow": allow, "deny_msg": deny_msg}
