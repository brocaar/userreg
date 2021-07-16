build:
	go build
	go vet

check:
	staticcheck

docker:
	docker build -t userreg:latest .

clean:
	go clean

