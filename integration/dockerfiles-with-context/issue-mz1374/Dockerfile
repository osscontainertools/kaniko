# mz1374: a RUN bind mount of a symlink source mounts the link instead of its target.
# Mounts the link since v1.27.3, cross-stage since 236826dbe behind FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT.
# context/value links to real/value in the context.
# The src stage links /value to /real/value, and the RUN stage has its own /real/value.
# A mounted absolute link would read the RUN stage's file.
# The context is passed as /workspace/../workspace, which SecureJoin rejects unless cleaned.
# The test steps stay because the context harness ignores file content.
FROM busybox AS src
RUN mkdir /real && echo stage-content > /real/value && ln -s /real/value /value

FROM busybox
RUN mkdir /real && echo wrong > /real/value
RUN --mount=type=bind,source=context/value,target=/mnt/value test "$(cat /mnt/value)" = context-content
RUN --mount=type=bind,from=src,source=/value,target=/mnt/stage test "$(cat /mnt/stage)" = stage-content
