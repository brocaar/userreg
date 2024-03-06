build:
	go build
	go vet

check:
	staticcheck

docker:
	docker build -t userreg-v4:latest .

clean:
	go clean

