GOPATH := /vagrant/go:${shell pwd}/vendor
export GOPATH

SOURCEDIR=.
SOURCES := $(shell find $(SOURCEDIR) -name '*.go')

BINARY=routed-plugin

BUILD = `git rev-parse HEAD`
BUILD_TIME=`date +%FT%T%z`

LDFLAGS=-ldflags "-X main.Build=${BUILD} -X main.BuildTime=${BUILD_TIME}"

IMAGETAG := test/routed-plugin

build: $(SOURCES)
	    go build ${LDFLAGS} -o ${BINARY} main.go

docker-build: build
	docker build -t $(IMAGETAG) .

clean:
	if [ -f ${BINARY} ] ; then rm ${BINARY} ; fi

docker-clean:
	-@docker rm $(docker ps -a -q) > /dev/null 2>&1
	-@docker rmi $(docker images | grep "^<none>" | awk '{print $3}') > /dev/null 2>&1

docker-run: docker-build
	docker run -ti --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins ${IMAGETAG} --debug

.DEFAULT_GOAL: build
.PHONY: clean, docker-clean
