FROM golang:1.22-alpine AS build
WORKDIR /app
COPY . .
RUN go build

FROM alpine:latest  
WORKDIR /app
COPY --from=build /app/userreg .
CMD ["./userreg", "userreg.conf"]
