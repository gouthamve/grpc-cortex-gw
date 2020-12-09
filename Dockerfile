FROM golang:1.15-alpine3.12 as build
RUN apk add --update --no-cache git
COPY . /src
WORKDIR /src
RUN go build -o grpc-cortex-gw


FROM alpine:3.12
RUN apk add --update --no-cache ca-certificates
COPY --from=build /src/grpc-cortex-gw /bin/grpc-cortex-gw
ENTRYPOINT [ "/bin/grpc-cortex-gw" ]
EXPOSE 8080
ARG revision
LABEL org.opencontainers.image.title="grpc-cortex-gw" \
      org.opencontainers.image.source="https://github.com/gouthamve/grpc-cortex-gw" \
      org.opencontainers.image.revision="${revision}"
