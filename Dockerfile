# Build the manager binary
FROM golang:1.22.12 AS builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY apis/ apis/
COPY pkg/ pkg/
COPY cloudprovider/ cloudprovider/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

# Use distroless as minimal base images to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:3.14
WORKDIR /
COPY --from=builder /workspace/manager .

ENTRYPOINT ["/manager"]
