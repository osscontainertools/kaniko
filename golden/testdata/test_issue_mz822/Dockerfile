# mz822: a COPY heredoc whose body expands a build arg is not reflected in the
# layer cache key. The body is omitted from the instruction text the key is built
# from, so a build that only changes the arg reuses the layer cached for a
# different resolved content and serves a stale file. Same defect as mz791 for
# COPY/ADD paths and mz813 for WORKDIR.
FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d
ARG A
COPY <<EOF /out.txt
value=${A}
EOF
