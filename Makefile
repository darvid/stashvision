.PHONY: clean

stashvision-go/stashvision.exe:
	docker run -v ${CURDIR}/stashvision-go:/usr/src/stashvision \
	  -w /usr/src/stashvision \
	  -e GOOS=windows \
	  -e GOARCH=amd64 \
	  golang:1.13 go build -v cmd/stashvision.go

clean:
	rm -f stashvision-go/stashvision.exe