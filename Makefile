export CGO_ENABLED:=0

PROJ=issue-sync
ORG_PATH=github.com/coreos
REPO_PATH=$(ORG_PATH)/$(PROJ)
VERSION=$(shell ./git-version)
BUILD_TIME=`date +%FT%T%z`
GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)
SOURCES := $(shell find . -name '*.go')
LD_FLAGS=-ldflags "-X $(REPO_PATH)/cmd.Version=$(VERSION)"

HAS_DEP := $(shell command -v dep;)


$(GOBIN):
	echo "create gobin"
	mkdir -p $(GOBIN)

work: $(GOBIN)

depend: work
ifndef HAS_DEP
	curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
endif
	dep ensure

depend-update: work
	dep ensure -update

build: bin/$(PROJ)

bin/$(PROJ): $(SOURCES)
	@go build -o bin/$(PROJ) $(LD_FLAGS) $(REPO_PATH)

clean:
	@rm bin/*

.PHONY: clean depend

.DEFAULT_GOAL: build
