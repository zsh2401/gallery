.PHONY: dev build test fmt clean install

install:
	npm install

dev:
	@test -d node_modules || npm install
	npm run dev

build:
	@test -d node_modules || npm install
	npm run build

test:
	@test -d node_modules || npm install
	npm test

fmt:
	@test -d node_modules || npm install
	npm run fmt

clean:
	rm -rf dist node_modules
