ARG GOLANG_IMAGE="golang:1.23.11"
ARG RUNNER_IMAGE="busybox:1.37.0"

FROM ${GOLANG_IMAGE} AS builder

COPY . /src/
WORKDIR /src/

ENV PATH="/src/go/bin:$PATH"
RUN go build -o /bin/cdserver cmd/cdserver/cdserver.go

FROM ${RUNNER_IMAGE}

EXPOSE 8083

ENV USER_ID=1000
ENV GROUP_ID=1000
ENV USER_NAME=cdserver
ENV GROUP_NAME=cdserver

RUN mkdir /data && chmod 755 /data && \
    addgroup -S -g $GROUP_ID $GROUP_NAME && \
    adduser -S -u $USER_ID -G $GROUP_NAME $USER_NAME && \
    chown -R $USER_NAME:$GROUP_NAME /data

USER $USER_NAME

COPY --from=builder /bin/cdserver /bin/cdserver
COPY config/config.yml /etc/cdserver.yml

ENTRYPOINT ["/bin/cdserver"]
CMD ["-config.file=/etc/cdserver.yml"]
