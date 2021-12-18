IMAGE=docker.io/iskorotkov/finance-dashboard-provider-stocks:v0.1.0

build-dev:
	docker build -f build/dev.dockerfile -t $(IMAGE)-dev .

push-dev:
	docker push $(IMAGE)-dev

watch:
	reflex -s -r '\.go$$' go run cmd/main.go

grpc-gen:
	protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative api/currency_service.proto
