package docker

default allow := false

allow if input.local
allow if input.image.repo == "alpine"

decision := {"allow": allow}
