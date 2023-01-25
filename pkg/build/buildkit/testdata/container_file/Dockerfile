FROM alpine:latest

RUN set -ex \
    && apk add --update --no-cache ca-certificates curl

COPY ./tsuru.yaml ./Procfile /app/user/

WORKDIR /app/user
