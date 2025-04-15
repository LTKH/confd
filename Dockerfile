ARG GOLANG_IMAGE="golang:1.18.3-alpine"
ARG ALPINE_IMAGE="alpine:3.21.3"

FROM ${GOLANG_IMAGE} AS builder

COPY . /src/
WORKDIR /src/

RUN apk add --no-cache --update go gcc g++
RUN CGO_ENABLED=1 go build -o /bin/cdserver cmd/cdserver/cdserver.go

FROM ${ALPINE_IMAGE}

EXPOSE 8083

ENV USER_ID=1000
ENV GROUP_ID=1000
ENV USER_NAME=cdserver
ENV GROUP_NAME=cdserver

RUN mkdir /data && chmod 755 /data && \
    addgroup -S -g $GROUP_ID $GROUP_NAME && \
    adduser -S -u $USER_ID -G $GROUP_NAME $USER_NAME && \
    chown -R $USER_NAME:$GROUP_NAME /data

COPY --from=builder /bin/cdserver /bin/cdserver
COPY config/config.yml /etc/cdserver.yml

USER $USER_NAME

ENTRYPOINT ["/bin/cdserver"]
CMD ["-config.file=/etc/cdserver.yml"]
