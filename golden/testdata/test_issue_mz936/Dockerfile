# base gets stored because it has a downstream consumer
FROM alpine:3.12 AS base
RUN touch /blubb

# shared1 stores as the image is shared with shared2
FROM alpine:3.11 AS shared1
RUN touch /blubb

# shared2 only loads the same image
FROM alpine:3.11 AS shared2
RUN touch /blubb

FROM base AS derived

# the unused stage will get streamed
FROM alpine:3.10 AS unused
RUN touch /blubb

# final stores iff it gets pushed
FROM debian:12.10 AS final
COPY --from=base /blubb /0
COPY --from=shared1 /blubb /1
COPY --from=shared2 /blubb /2
COPY --from=unused /blubb /3
COPY --from=derived /blubb /4
