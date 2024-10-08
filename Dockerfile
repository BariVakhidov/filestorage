# syntax=docker/dockerfile:1

# Build the application from source
FROM golang:1.22.2 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -o /filestorage

# Run the tests in the container
FROM build-stage AS run-test-stage
RUN go test -v ./...

# Deploy the application binary into a lean image
FROM gcr.io/distroless/base-debian11 AS build-release-stage

WORKDIR /

COPY --from=build-stage /filestorage /filestorage
COPY --from=build-stage /app/certs /certs
EXPOSE 8080

USER root:root

ENTRYPOINT ["/filestorage"]