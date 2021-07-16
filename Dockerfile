FROM golang:1.16-alpine AS build
WORKDIR /app
COPY . .
RUN GOPROXY=off CGO_ENABLED=0 GOOS=linux go build

FROM alpine:latest  
WORKDIR /app
COPY --from=build /app/userreg .
CMD ["./userreg", "userreg.conf"]
