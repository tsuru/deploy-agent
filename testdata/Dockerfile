FROM docker.io/tsuru/scratch:latest

COPY ./application.tar.gz /home/application/archive.tar.gz
ADD  ./application.tar.gz /home/application/current
RUN set -x \
    && { mkdir /tmp/foo ; } \
    && { echo "Hello world" > /tmp/foo/bar ; } \
    && :

WORKDIR /home/application/current
