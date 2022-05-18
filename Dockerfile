FROM golang:1.18.1-alpine as build
RUN apk add git openssh-client gcc musl-dev

ARG GITHUB_TOKEN

COPY . /usr/src/code
WORKDIR /usr/src/code

RUN git config --global url."https://${GITHUB_TOKEN}@github.com/".insteadOf "ssh://git@github.com/"
RUN git config --global url."https://${GITHUB_TOKEN}@github.com/".insteadOf "https://github.com/"

RUN export GOPRIVATE=github.com/momentum-xyz
RUN go build -race -o ./bin/controller ./apps/service

#FROM alpine:latest as production-build
FROM golang:1.18.1-alpine as production-build

RUN apk add gcc musl-dev gd-dev
RUN go install github.com/go-delve/delve/cmd/dlv@latest

RUN apk add --update --no-cache supervisor && rm -rf /var/cache/apk/*

RUN mkdir /opt/controller
COPY --from=build /usr/src/code/bin/controller /opt/code/controller
ADD docker_assets/supervisord.conf /etc/supervisord.conf

# This command runs your application, comment out this line to compile only
CMD ["/usr/bin/supervisord","-n", "-c", "/etc/supervisord.conf"]

LABEL Name=pengine Version=0.0.1
