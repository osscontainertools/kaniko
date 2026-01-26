FROM busybox AS base
RUN touch /blubb

FROM base AS final
COPY --from=base /blubb /blubb
RUN ls -lah /blubb
