FROM debian:12.10 AS first-stage

FROM first-stage AS second-stage

FROM first-stage AS third-stage
RUN touch test

# When we want to squash the 4th stage
# into the 2nd we must reorder the stages
# as otherwise the dependency on the 3rd stage
# could not be fulfilled.
FROM second-stage AS fourth-stage
COPY --from=third-stage test test

# When we optimize stages out this should not
# impact squashing logic at all
FROM debian:12.10 as noise
FROM fourth-stage AS fifth-stage
