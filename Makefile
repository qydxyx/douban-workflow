BINARY := douban-workflow
WORKFLOW := douban.alfredworkflow

.PHONY: all build package clean

all: package

build:
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY)-arm64 douban.go
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-amd64 douban.go
	codesign -f -s - $(BINARY)-arm64
	codesign -f -s - $(BINARY)-amd64
	lipo -create -output $(BINARY) $(BINARY)-arm64 $(BINARY)-amd64
	rm -f $(BINARY)-arm64 $(BINARY)-amd64

package: build
	rm -f $(WORKFLOW)
	zip -r $(WORKFLOW) $(BINARY) icon.png imgs info.plist -x "*.DS_Store"

clean:
	rm -f $(BINARY) $(BINARY)-arm64 $(BINARY)-amd64 $(WORKFLOW)
