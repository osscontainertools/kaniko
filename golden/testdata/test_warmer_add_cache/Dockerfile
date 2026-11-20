ARG URL=https://example.com/archive.tar.gz
FROM debian:bookworm
ENV BASE=https://example.com FILE=file.txt
ADD $BASE/$FILE /file/
ADD https://example.com/archive.tar.gz /one/
ADD $URL /two/
ADD --checksum=sha256:1111111111111111111111111111111111111111111111111111111111111111 https://example.com/pinned.bin /pinned/
ADD local.txt /local/
