# syntax=docker/dockerfile:1

# Build: docker build -f server/Dockerfile --build-arg VERSION=$(git describe --always) -t zazzzor-api server
FROM golang:1.27.4 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/zazzzor ./cmd/zazzzor
# An empty /data owned by nonroot: a fresh named volume mounted there copies
# this ownership, so the server can write images into it.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/zazzzor /zazzzor
COPY --from=build --chown=nonroot:nonroot /out/data /data
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/zazzzor"]
