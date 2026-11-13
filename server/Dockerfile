# One image for every role: api, worker, watch, migrate (ATLAS.md §4.2). Distroless, non-root, ~20 MB.
FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# GO_TAGS is empty in production. Development stacks pass devlicense, so a throwaway license key can be used.
ARG GO_TAGS=""
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -tags "${GO_TAGS}" \
      -ldflags "-s -w -X github.com/morse/atlas/server/app.Version=${VERSION}" \
      -o /out/atlas ./cmd/atlas \
 && mkdir -p /out/data
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/fakemodels ./cmd/fakemodels

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS runtime
COPY --from=build /out/atlas /atlas
# A named volume mounted on /data inherits this ownership, so the non-root user can write blobs.
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
ENTRYPOINT ["/atlas"]

# Stand-in model servers for development and tests (deploy/compose, profile "fake"). Never the final stage and
# never built for production: `docker build --target prod` does not contain /fakemodels.
FROM runtime AS fake
COPY --from=build /out/fakemodels /fakemodels

# The last stage is what a plain `docker build` produces: the production image.
FROM runtime AS prod
