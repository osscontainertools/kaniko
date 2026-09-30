# mz1133: the --link copies keep one key whatever the salted RUN above them
# did, the plain copy beside them moves with the salt, and /after shows what
# the chain carried on with. The cross-stage one goes last, a redirect miss
# ends the lookahead for the rest of the stage. builder stays unsalted so its
# stage key, which the redirect key is built from, does not move either.
FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d AS builder
RUN echo built > /out

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
COPY --link --from=builder /out /xlink
