FROM ubuntu:24.04@sha256:68ee22004a9109b44973313b02ff7b1de250f1de0f5ecb628d73f579aaff33ac AS unused

FROM debian:13.7@sha256:4c25275446ca371766b8b093c696782eabce06263fccacb8d5d813b3dc44b308 AS app
RUN echo app > /app.txt
RUN --mount=type=bind,from=alpine:3.22.4@sha256:310c62b5e7ca5b08167e4384c68db0fd2905dd9c7493756d356e893909057601,source=/etc/alpine-release,target=/alpine-release \
    cp /alpine-release /alpine-release.txt
