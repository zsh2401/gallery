.PHONY: dev dev-backend dev-frontend setup build test fmt sample-gallery clean

dev:
	@set -e; \
	cleanup() { kill $$BACKEND_PID $$FRONTEND_PID 2>/dev/null || true; wait $$BACKEND_PID $$FRONTEND_PID 2>/dev/null || true; }; \
	$(MAKE) -C backend dev & BACKEND_PID=$$!; \
	$(MAKE) -C frontend dev & FRONTEND_PID=$$!; \
	trap 'cleanup; exit 0' INT TERM; \
	trap 'cleanup' EXIT; \
	wait

dev-backend:
	$(MAKE) -C backend dev

dev-frontend:
	$(MAKE) -C frontend dev

setup:
	cd backend && go mod download
	cd frontend && npm install

build:
	$(MAKE) -C backend build
	$(MAKE) -C frontend build

test:
	$(MAKE) -C backend test
	$(MAKE) -C frontend test

fmt:
	$(MAKE) -C backend fmt
	$(MAKE) -C frontend fmt

sample-gallery:
	GO111MODULE=off go run ./tools/make-sample-gallery.go --out ./.sample-gallery

clean:
	$(MAKE) -C backend clean
	rm -rf frontend/dist .sample-gallery
