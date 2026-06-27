FROM alpine AS base
RUN install

FROM base AS app
RUN build app

FROM base AS tools
RUN build tools
