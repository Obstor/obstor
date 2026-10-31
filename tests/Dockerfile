FROM ubuntu:latest

ENV DEBIAN_FRONTEND=noninteractive
ENV LANG=C.UTF-8
ENV GOROOT=/usr/local/go
ENV GOPATH=/usr/local/gopath
ENV PATH=$GOPATH/bin:$GOROOT/bin:$PATH
ENV TESTS_ROOT_DIR=/tests

RUN apt -y update && apt -y upgrade && \
    apt -y --quiet install wget jq curl git dnsmasq

COPY . /tests

WORKDIR /tests

RUN /tests/create-data-files.sh
RUN /tests/preinstall.sh
RUN /tests/release.sh

ENTRYPOINT ["/tests/entrypoint.sh"]
