FROM debian:12.10 AS first-stage

# mz523: We can't squash second+first stages,
# but when we switch the context we don't need
# to clean the filesystem and unpack the image again.

FROM first-stage AS second-stage
RUN touch test

FROM second-stage AS third-stage
RUN touch test2

FROM first-stage AS fourth-stage
COPY --from=second-stage test test
COPY --from=third-stage test2 test2
