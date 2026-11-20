# mz1427: a single stage pushed to the registry its base came from mounts the
# base layers, so the push does not read the base again and the base streams.
ARG BASE=registry.example.com/team/base:1
FROM ${BASE}
RUN true
