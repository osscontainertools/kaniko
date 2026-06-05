FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d AS first
RUN touch /blubb

FROM first AS second
RUN touch /bla

FROM second AS third
RUN touch /bli

FROM second AS final
COPY --from=first /blubb /blubb
COPY --from=third /bli /bli
RUN ls -lah /blubb
