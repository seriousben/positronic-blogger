#!/bin/bash

set -euo pipefail

rm -rf .secrets
mkdir -p .secrets
trap "rm -rf .secrets" EXIT

echo -n "$NEWSBLUR_USERNAME" > .secrets/newsblur_username
echo -n "$NEWSBLUR_PASSWORD" > .secrets/newsblur_password
echo -n "$GITHUB_TOKEN" > .secrets/github_token

kubectl create secret generic newsblur --from-file=username=.secrets/newsblur_username --from-file=password=.secrets/newsblur_password

kubectl create secret generic github --from-file=token=.secrets/github_token
