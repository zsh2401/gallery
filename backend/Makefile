.PHONY: dev build test tidy fmt clean

dev:
	go run github.com/air-verse/air@latest -c .air.toml

build:
	go build -o ./bin/seymours-gallery ./

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	gofmt -w .

clean:
	rm -rf ./bin ./tmp
