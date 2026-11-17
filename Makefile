.PHONY: build frontend test dev-web

frontend:
	npm ci --prefix web
	npm run build --prefix web

build: frontend
	go build -o startshipd ./cmd/startshipd
	go build -o startship ./cmd/startship

test: frontend
	npm test --prefix web
	go test ./...

dev-web:
	npm run dev --prefix web
