.POSIX:
.SUFFIXES:
.PHONY: debug release vet clean version staticcheck revive dockerbuild docker push all tag upload
.SILENT: version dockerbuild

SOURCES = $(shell find . -type f -name '*.go' -not -path "./vendor/*")
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
	dep ensure
	./retool do go-bindata static/
	git add .
	git commit -m "Version ${VERSION}"
	git tag -a ${VERSION} -m "Version ${VERSION}"
	git push
	git push --tags

upload: all
	./retool do github-release ${VERSION} reaper_linux_amd64 reaper_linux_arm reaper_linux_arm64 reaper_openbsd_amd64 reaper_freebsd_amd64 --tag ${VERSION} --github-repository stephane-martin/reaper --github-access-token ${GITHUB_TOKEN}

${BINARY}_debug: ${SOURCES} ${STATICFILES} model_gen.go .tools_sync
	dep ensure
	./retool do go-bindata -debug static/
	CGO_ENABLED=0 go build -x -tags 'netgo osusergo' -o ${BINARY}_debug ${LDFLAGS} ${FULL}

${BINARY}: ${SOURCES} ${STATICFILES} model_gen.go .tools_sync
	dep ensure
	./retool do go-bindata static/
	CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY} ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_openbsd_amd64: ${SOURCES} ${STATICFILES} model_gen.go .tools_sync
	dep ensure
	./retool do go-bindata static/
	GOOS=openbsd GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_openbsd_amd64 ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_freebsd_amd64: ${SOURCES} ${STATICFILES} model_gen.go .tools_sync
	dep ensure
	./retool do go-bindata static/
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_freebsd_amd64 ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_linux_amd64: ${SOURCES} ${STATICFILES} model_gen.go .tools_sync
	dep ensure
	./retool do go-bindata static/
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_linux_amd64 ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_linux_arm: ${SOURCES} ${STATICFILES} model_gen.go .tools_sync
	dep ensure
	./retool do go-bindata static/
	GOOS=linux GOARCH=arm CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_linux_arm ${LDFLAGS_RELEASE} ${FULL}

${BINARY}_linux_arm64: ${SOURCES} ${STATICFILES} model_gen.go .tools_sync
	dep ensure
	./retool do go-bindata static/
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -a -installsuffix nocgo -tags 'netgo osusergo' -o ${BINARY}_linux_arm64 ${LDFLAGS_RELEASE} ${FULL}


all: ${BINARY}_openbsd_amd64 ${BINARY}_freebsd_amd64 ${BINARY}_linux_amd64 ${BINARY}_linux_arm ${BINARY}_linux_arm64

retool:
	go get -u github.com/twitchtv/retool
	cp ${GOPATH}/bin/retool .

.tools_sync: retool tools.json
	./retool sync
	touch .tools_sync

model_gen.go: .tools_sync model.go
	./retool do msgp -file model.go

dockerbuild:
	bash dockerbuild.sh

docker:
	docker build -t reaper:${VERSION} .

clean:
	rm -f ${BINARY} ${BINARY}_debug

version:
	echo ${VERSION}

vet:
	go vet ./...

staticcheck: .tools_sync
	./retool do staticcheck ./...

revive: .tools_sync
	./retool do revive -formatter stylish -exclude vendor/... ./...
