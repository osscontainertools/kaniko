FROM debian:13.7@sha256:4c25275446ca371766b8b093c696782eabce06263fccacb8d5d813b3dc44b308 AS first
RUN echo first > /first.txt
FROM alpine:3.12@sha256:cb64bbe7fa613666c234e1090e91427314ee18ec6420e9426cf4e7f314056813
COPY --from=first /first.txt /first.txt
