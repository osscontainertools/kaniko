# base gets stored because it has a downstream consumer
FROM alpine@sha256:c75ac27b49326926b803b9ed43bf088bc220d22556de1bc5f72d742c91398f69 AS base
RUN touch /blubb

# shared1 stores as the image is shared with shared2
FROM alpine@sha256:bcae378eacedab83da66079d9366c8f5df542d7ed9ab23bf487e3e1a8481375d AS shared1
RUN touch /blubb

# shared2 only loads the same image
FROM alpine@sha256:bcae378eacedab83da66079d9366c8f5df542d7ed9ab23bf487e3e1a8481375d AS shared2
RUN touch /blubb

FROM base AS derived

# the unused stage will get streamed
FROM alpine@sha256:451eee8bedcb2f029756dc3e9d73bab0e7943c1ac55cff3a4861c52a0fdd3e98 AS unused
RUN touch /blubb

# extshared stores as the image is also read by a COPY --from below
FROM alpine:3.13 AS extshared
RUN touch /blubb

# final stores iff it gets pushed
FROM debian@sha256:264982ff4d18000fa74540837e2c43ca5137a53a83f8f62c7b3803c0f0bdcd56 AS final
COPY --from=base /blubb /0
COPY --from=shared1 /blubb /1
COPY --from=shared2 /blubb /2
COPY --from=unused /blubb /3
COPY --from=derived /blubb /4
COPY --from=extshared /blubb /5
COPY --from=alpine:3.13 /etc/os-release /6
# streams, nothing else reads it
COPY --from=debian:10.13 /etc/os-release /7
