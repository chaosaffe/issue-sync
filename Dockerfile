FROM alpine:3.6

WORKDIR /opt/issue-sync

RUN apk update --no-cache && apk add ca-certificates

COPY bin/issue-sync /opt/issue-sync/issue-sync

COPY config.yaml /opt/issue-sync/config.yaml

ENTRYPOINT ["./issue-sync"]

CMD ["--config", "config.yaml"]
