# mz1133: the --link copy keeps one key whatever the salted RUN above it did,
# the plain copy beside it moves with the salt, and the trailing COPY shows
# what the chain carried on with.
FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d
ARG SALT
RUN echo "${SALT}" > /salt
COPY <<EOF /plain/foo
hello
EOF
COPY --link <<EOF /link/foo
hello
EOF
COPY <<EOF /after
hello
EOF
