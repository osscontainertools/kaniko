FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d AS common
RUN touch /common

FROM common AS first
RUN touch /first

FROM common AS second
RUN touch /second

FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d AS meta
ENV FOO=bar

FROM meta AS third
RUN touch /third

FROM meta AS fourth
RUN touch /fourth

FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d
COPY --from=first /first /first
COPY --from=second /second /second
COPY --from=third /third /third
COPY --from=fourth /fourth /fourth
