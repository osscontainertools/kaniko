package docker

default allow := false

allow if input.local

decision := {"allow": allow}
