FROM debian:12.10 AS base
RUN install

FROM base AS build
RUN compile

FROM base AS final
COPY --from=build output output

FROM final AS test
RUN test