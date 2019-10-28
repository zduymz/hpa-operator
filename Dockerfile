FROM alpine:latest
LABEL maintainer="duymai"

RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
COPY ./build/linux/hpa-operator /bin/hpa-operator

USER nobody

ENTRYPOINT ["/bin/hpa-operator"]

