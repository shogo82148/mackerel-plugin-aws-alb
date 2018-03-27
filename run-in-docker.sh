#!/bin/sh

CURRENT=$(cd "$(dirname "$0")" && pwd)
docker run --rm -it \
    -v "$CURRENT":/go/src/github.com/shogo82148/mackerel-plugin-aws-alb \
    -w /go/src/github.com/shogo82148/mackerel-plugin-aws-alb golang:1.10 "$@"
