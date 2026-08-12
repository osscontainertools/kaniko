FROM debian@sha256:264982ff4d18000fa74540837e2c43ca5137a53a83f8f62c7b3803c0f0bdcd56 AS base
RUN install

FROM base AS build
RUN compile

FROM base AS final
COPY --from=build output output

FROM final AS test
RUN test
