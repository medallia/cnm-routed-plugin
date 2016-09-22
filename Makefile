GOPATH:=/vagrant/go:$(shell pwd)/vendor
export GOPATH

SOURCEDIR=.
SOURCES=$(shell find $(SOURCEDIR) -name '*.go')

BATSBIN=$(shell which bats 2>/dev/null)
BATSDIR=.
BATSFILES=$(shell find $(BATSDIR) -name '*.bats')

BINARY=routed-plugin

BUILD=`git rev-parse HEAD`
BUILD_TIME=`date +%FT%T%z`

LDFLAGS=-ldflags "-X main.Build=$(BUILD) -X main.BuildTime=$(BUILD_TIME)"

IMAGETAG=routed-plugin

build: $(SOURCES)
	go build $(LDFLAGS) -o $(BINARY) main.go

clean:
	if [ -f $(BINARY) ] ; then rm $(BINARY) ; fi

docker-build: build
	docker build -t $(IMAGETAG) .

docker-run: 
	docker run -ti --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins $(IMAGETAG) --gateway 10.100.0.1 --mtu 9000 --debug

docker-clean:
	docker rm $(docker ps -aq) > /dev/null 2>&1
	docker rmi $(docker images | grep "^<none>" | awk '{print $3}') > /dev/null 2>&1

coverage:
	sudo GOPATH=$(GOPATH) PATH=$(PATH) /bin/bash -c '( GO_ENV=test go test -v -coverprofile=coverage.out ./routed && go tool cover -html=coverage.out )'

tests:
	sudo GOPATH=$(GOPATH) PATH=$(PATH) /bin/bash -c 'go test -v ./routed'

integration: docker-build 
	$(BATSBIN) $(BATSFILES)

.DEFAULT_GOAL: build
.PHONY: build, clean, docker-build, docker-run, docker-clean, coverage, tests, test-one, integration
