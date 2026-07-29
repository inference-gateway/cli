# Build context is the core/cli module root:
#   docker build -f tests/mockgateway/Dockerfile -t mock-gateway:local .
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mock-gateway ./tests/mockgateway

FROM gcr.io/distroless/static:nonroot
COPY --from=build /mock-gateway /mock-gateway
ENTRYPOINT ["/mock-gateway"]
