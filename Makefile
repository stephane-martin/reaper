.POSIX:
.SUFFIXES:
.PHONY: debug release vet clean version staticcheck revive
.SILENT: version

SOURCES = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
DATAFILES = $(shell find data -type f)

BINARY=reaper
FULL=github.com/stephane-martin/reaper
VERSION=0.1.0
LDFLAGS=-ldflags '-X main.Version=${VERSION} -X services.GinMode=debug'
LDFLAGS_RELEASE=-ldflags '-w -s -X main.Version=${VERSION} -X services.GinMode=release'

debug: ${BINARY}_debug
release: ${BINARY}

vet:
	go vet ./...

staticcheck: .tools_sync
	./retool do staticcheck ./...

revive: .tools_sync
	./retool do revive -formatter stylish -exclude vendor/... ./...

clean:
	rm -f ${BINARY} ${BINARY}_debug

version:
	echo ${VERSION}

${BINARY}_debug: ${SOURCES}
	dep ensure
	CGO_ENABLED=0 go build -x -tags 'netgo osusergo' -o ${BINARY}_debug ${LDFLAGS} ${FULL}

${BINARY}: ${SOURCES}
	dep ensure
	CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY} ${LDFLAGS_RELEASE} ${FULL}

retool:
	go get -u github.com/twitchtv/retool
	cp ${GOPATH}/bin/retool .

.tools_sync: retool tools.json
	./retool sync
	touch .tools_sync

