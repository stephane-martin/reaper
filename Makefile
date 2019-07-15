.SUFFIXES:
.PHONY: debug release clean version revive push all tag upload

SOURCES = $(shell find . -type f -name '*.go')
STATICFILES = $(shell find static -type f)
GITHUB_TOKEN = $(shell cat token)

BINARY=reaper
FULL=github.com/stephane-martin/reaper
VERSION=0.1.0
LDFLAGS=-ldflags '-X main.Version=${VERSION} -X main.GinMode=debug'
LDFLAGS_RELEASE=-ldflags '-w -s -X main.Version=${VERSION} -X main.GinMode=release'

debug: ${BINARY}_debug
release: ${BINARY}

tag:
	git add .
	git commit -m "Version ${VERSION}"
	git tag -a ${VERSION} -m "Version ${VERSION}"
	git push
	git push --tags

upload: all
	github-release ${VERSION} reaper_linux_amd64 reaper_linux_arm reaper_linux_arm64 reaper_openbsd_amd64 reaper_freebsd_amd64 --tag ${VERSION} --github-repository stephane-martin/reaper --github-access-token ${GITHUB_TOKEN}

${BINARY}_debug: ${SOURCES} ${STATICFILES} model_gen.go bindata.go 
	CGO_ENABLED=0 go build -x -tags 'netgo osusergo' -o ${BINARY}_debug ${LDFLAGS} ${FULL}

${BINARY}: ${SOURCES} ${STATICFILES} model_gen.go bindata.go
	CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY} ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_openbsd_amd64: ${SOURCES} ${STATICFILES} model_gen.go bindata.go
	GOOS=openbsd GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_openbsd_amd64 ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_freebsd_amd64: ${SOURCES} ${STATICFILES} model_gen.go bindata.go
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_freebsd_amd64 ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_linux_amd64: ${SOURCES} ${STATICFILES} model_gen.go bindata.go
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_linux_amd64 ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_linux_arm: ${SOURCES} ${STATICFILES} model_gen.go bindata.go
	GOOS=linux GOARCH=arm CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_linux_arm ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_linux_arm64: ${SOURCES} ${STATICFILES} model_gen.go bindata.go
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_linux_arm64 ${LDFLAGS_RELEASE} ${FULL}

all: ${BINARY}_openbsd_amd64 ${BINARY}_freebsd_amd64 ${BINARY}_linux_amd64 ${BINARY}_linux_arm ${BINARY}_linux_arm64 README.rst

model_gen.go: model.go
	msgp -file model.go

bindata.go: static/stream.html
	go-bindata -fs static/

clean:
	rm -f ${BINARY} ${BINARY}_debug

revive: 
	revive -formatter stylish ./...

README.rst: docs/README.md
	pandoc --from markdown --to rst --toc --number-sections --standalone -o README.rst docs/README.md


