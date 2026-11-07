FROM alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS base
RUN install

FROM base AS app
RUN build app

FROM base AS tools
RUN build tools
