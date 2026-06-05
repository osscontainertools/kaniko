# mz762: reproduces github.com/vidbregar/kaniko-allowlist-bug — an allowlist
# .dockerignore (`*` then `!test.txt`) with a multi-stage COPY --from.
FROM alpine:3.22.4@sha256:310c62b5e7ca5b08167e4384c68db0fd2905dd9c7493756d356e893909057601 AS builder

WORKDIR app

RUN apk add curl

COPY . .

FROM alpine:3.22.4@sha256:310c62b5e7ca5b08167e4384c68db0fd2905dd9c7493756d356e893909057601 AS final

WORKDIR /app

COPY --from=builder /app .

RUN addgroup -g 10001 app && \
    adduser -u 10001 -G app -D -h /app -s /bin/ash app && \
    chown -R app:app /app

USER app:app
