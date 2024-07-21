FROM golang:1.20.3 AS builder

COPY . /src/
WORKDIR /src/
RUN go build -o /bin/cdserver cmd/cdserver/cdserver.go

FROM alpine:latest

EXPOSE 8084

ENV USER_ID=1000
ENV USER_NAME=cdserver

RUN mkdir /data && chmod 755 /data && \
    adduser -D -u $USER_ID -h /data $USER_NAME && \
    chown -R $USER_NAME /data

COPY --from=builder /bin/cdserver /bin/cdserver
COPY config/config.yml /etc/cdserver.yml

USER $USER_NAME

ENTRYPOINT ["/bin/cdserver"]
CMD ["-config.file=/etc/cdserver.yml"]