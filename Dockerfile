FROM golang:1.25-alpine AS build_deps

RUN apk add --no-cache git

WORKDIR /workspace

COPY go.mod .
COPY go.sum .

RUN go mod download

FROM build_deps AS build

COPY . .

RUN CGO_ENABLED=0 go build -o webhook \
    -ldflags "-w -extldflags '-static' -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo unknown)" .

FROM alpine:3.23

RUN apk upgrade --no-cache && apk add --no-cache ca-certificates

COPY --from=build /workspace/webhook /usr/local/bin/webhook

ENTRYPOINT ["webhook"]
