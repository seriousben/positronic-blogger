#!/bin/bash

set -euo pipefail

docker run --rm -v "$PWD":/usr/local/go/src/github.com/seriousben/newsblur-to-hugo -w /usr/local/go/src/github.com/seriousben/newsblur-to-hugo golang:1.7.5-alpine ./build.sh
docker build -t seriousben/newsblur-to-hugo:latest .
docker push seriousben/newsblur-to-hugo:latest

kubectl apply --force -f deploy.yml
