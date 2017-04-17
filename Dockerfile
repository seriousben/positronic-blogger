FROM alpine:latest
RUN apk add --no-cache --update ca-certificates
ADD main /app/main
ADD short.tmpl /app/short.tmpl

WORKDIR /app
ENTRYPOINT /app/main
