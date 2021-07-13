FROM golang:1.16-alpine AS build
WORKDIR /app
COPY . .
RUN GOPROXY=off CGO_ENABLED=0 GOOS=linux go build

FROM alpine:latest  
# RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /app/userreg .
CMD ["./userreg"]  
