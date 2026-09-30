# mz1133: --link keys the copy on itself, so it is looked up under one key
# whatever the salted RUN above it did. The plain copy beside it is keyed on the
# chain and moves with the salt. The trailing RUN shows what the chain carries
# on with: the link key with the flag on, the copy's own inputs with it off.
FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d
ARG SALT
RUN echo "${SALT}" > /salt
COPY <<EOF /plain/foo
hello
EOF
COPY --link <<EOF /link/foo
hello
EOF
RUN cat /link/foo
