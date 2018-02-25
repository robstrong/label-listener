# build stage
FROM golang:1.10 AS build-env
ADD . /go/src/github.com/robstrong/label-listener/
RUN cd /go/src/github.com/robstrong/label-listener/ && CGO_ENABLED=0 go build -o goapp main.go

# final stage
FROM alpine
COPY --from=build-env /go/src/github.com/robstrong/label-listener/goapp /app/
ENTRYPOINT /app/goapp