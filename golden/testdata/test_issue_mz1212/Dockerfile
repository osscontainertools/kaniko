FROM alpine:3.20 AS builder

FROM debian:bookworm
COPY --from=builder /etc/os-release /0
COPY --from=0 /etc/os-release /1
COPY --from=alpine:3.20 /etc/os-release /2
COPY --from=busybox:1.36 /bin/busybox /3
COPY --from=busybox:1.36 /bin/busybox /4
