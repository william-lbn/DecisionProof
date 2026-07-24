.PHONY: fmt vet test race build demo product-demo verify archive

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test -cover ./...

race:
	go test -race ./...

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o bin/control-api ./cmd/control-api
	CGO_ENABLED=0 go build -trimpath -o bin/migrate ./cmd/migrate
	CGO_ENABLED=0 go build -trimpath -o bin/demo ./cmd/demo
	CGO_ENABLED=0 go build -trimpath -o bin/product-demo ./cmd/product-demo

demo:
	go run ./cmd/demo

product-demo:
	go run ./cmd/product-demo

verify: fmt vet test race build
	./bin/demo >/tmp/sdlcproof-demo.json
	./bin/product-demo >/tmp/decisionproof-product-demo.json
	@echo "验证完成，演示结果位于 /tmp/sdlcproof-demo.json"
