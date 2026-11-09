# One image for every role: api, worker, watch, migrate (ATLAS.md §4.2). Distroless, non-root, ~20 MB.
FROM golang:1.27.3-bookworm@sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/morse/atlas/server/app.Version=${VERSION}" \
      -o /out/atlas ./cmd/atlas \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/fakemodels ./cmd/fakemodels \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/atlas /atlas
# Stand-in model servers for development and tests (deploy/compose, profile "fake"). Not used in production.
COPY --from=build /out/fakemodels /fakemodels
# A named volume mounted on /data inherits this ownership, so the non-root user can write blobs.
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
ENTRYPOINT ["/atlas"]
