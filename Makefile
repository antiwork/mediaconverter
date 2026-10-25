build:
	@echo "Building mediaconverter..."
	cd src && go build -o ../mediaconverter

test:
	cd src && MAX_SIMULTANEOUS_JOBS=1 WEBHOOK_SECRET=test AWS_REGION=us-east-1 go test -race ./...

.PHONY: build test
