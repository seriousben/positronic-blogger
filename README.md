# positronic-blogger

positronic-blogger - A tool to assemble different sources into a markdown blog.

## Usage

```
POSITRONIC_NEWSBLUR_USERNAME=<nb_username> \
POSITRONIC_NEWSBLUR_PASSWORD=<nb password> \
POSITRONIC_GITHUB_TOKEN=<gh token> \
POSITRONIC_GITHUB_REPO=<ghorg/repo\
POSITRONIC_NEWSBLUR_CONTENT_PATH=content/links \
POSITRONIC_NEWSBLUR_CHECKPOINT_PATH=content/links/checkpoint \
POSITRONIC_SKIP_MERGE=true \
go run ./cmd/...
```