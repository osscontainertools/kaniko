package docker

default allow := false

allow if input.local
allow if input.http.host == "127.0.0.1"

decision := {"allow": allow}
