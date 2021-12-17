FROM golang:1.17-alpine
WORKDIR /app

# Install dependencies.
COPY ["go.mod", "go.sum", "./"]
RUN go get ./...

# Copy source files and build.
COPY ["./", "./"]
RUN go build -o app ./cmd/main.go

ENTRYPOINT ["./app"]
