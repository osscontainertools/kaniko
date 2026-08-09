FROM debian@sha256:264982ff4d18000fa74540837e2c43ca5137a53a83f8f62c7b3803c0f0bdcd56 AS first-stage

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
FROM debian@sha256:264982ff4d18000fa74540837e2c43ca5137a53a83f8f62c7b3803c0f0bdcd56 as noise
FROM fourth-stage AS fifth-stage
