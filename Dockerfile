ARG GOLANG_IMAGE="golang:latest"
ARG REDHAT_IMAGE="redhat/ubi8:latest"

FROM ${GOLANG_IMAGE} AS builder

COPY . /src/
WORKDIR /src/

RUN go build -o /bin/cdserver cmd/cdserver/cdserver.go

FROM ${REDHAT_IMAGE}

EXPOSE 8084

ENV USER_ID=1000
ENV GROUP_ID=1000
ENV USER_NAME=cdserver
ENV GROUP_NAME=cdserver

RUN mkdir /data && chmod 755 /data && \
    groupadd --gid $GROUP_ID $GROUP_NAME && \
    useradd -M --uid $USER_ID --gid $GROUP_ID --home /data $USER_NAME && \
    chown -R $USER_NAME:$GROUP_NAME /data

COPY --from=builder /bin/cdserver /bin/cdserver
COPY config/config.yml /etc/cdserver.yml

USER $USER_NAME

ENTRYPOINT ["/bin/cdserver"]
CMD ["-config.file=/etc/cdserver.yml"]
