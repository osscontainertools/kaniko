FROM alpine AS base
RUN install

FROM base AS intermediate
RUN install more

# mz487: in kaniko v1.26.1-v1.26.4 we had a regression whereby
# the build stage would not be squashed into the intermediate stage,
# as it was a pure CopyDependency.
FROM intermediate AS build
RUN compile

FROM base AS final
COPY --from=build output output
