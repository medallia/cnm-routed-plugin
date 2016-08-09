FROM golang
RUN go get github.com/tools/godep
COPY . /go/src/github.com/medallia/docker-routed-plugin
WORKDIR /go/src/github.com/medallia/docker-routed-plugin
RUN godep go install -v
ENTRYPOINT ["docker-routed-plugin"]
