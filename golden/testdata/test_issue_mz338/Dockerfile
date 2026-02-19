FROM alpine AS base
# mz338: kaniko v1.26.0 would squash base & build stages together.
# Instead of running the ONBUILD instruction here in between the stages,
# it would append it as a ONBUILD instruction to the resulting image.
ONBUILD RUN touch /blubb
RUN rm -rf /blubb

FROM base
RUN ls -lah /blubb
ONBUILD RUN echo blubb
