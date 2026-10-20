# syntax=docker/dockerfile:1

# Build: docker build -f server/Dockerfile --build-arg VERSION=$(git describe --always) -t zazzzor-api server
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/zazzzor ./cmd/zazzzor

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/zazzzor /zazzzor
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/zazzzor"]
