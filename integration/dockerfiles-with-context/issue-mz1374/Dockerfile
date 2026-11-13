FROM busybox AS src
RUN mkdir /real && echo stage-content > /real/value && ln -s /real/value /value

FROM busybox
RUN mkdir /real && echo wrong > /real/value

# Symlinked bind-mount source must resolve to the target contents.
RUN --mount=type=bind,source=context/value,target=/mnt/value test "$(cat /mnt/value)" = context-content
# An absolute link resolves inside the source stage, not the stage running the RUN.
RUN --mount=type=bind,from=src,source=/value,target=/mnt/stage test "$(cat /mnt/stage)" = stage-content
