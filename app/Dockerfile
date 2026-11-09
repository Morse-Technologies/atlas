# The web container (ATLAS.md §4.2): the Next.js app as a standalone server, behind Caddy on the same origin.
# Build: docker build --build-arg NEXT_PUBLIC_ATLAS_EDITION=pro -t atlas-web app
FROM node:22-bookworm-slim AS build
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
# The edition is fixed at build time: Core builds do not contain the Pro screens (src/lib/edition.ts).
ARG NEXT_PUBLIC_ATLAS_EDITION=core
# "live" talks to the Go server through Caddy; without it the app runs on demo data.
ARG NEXT_PUBLIC_ATLAS_API=live
ENV NEXT_PUBLIC_ATLAS_EDITION=${NEXT_PUBLIC_ATLAS_EDITION} NEXT_PUBLIC_ATLAS_API=${NEXT_PUBLIC_ATLAS_API} NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

FROM gcr.io/distroless/nodejs22-debian12:nonroot
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000
COPY --from=build --chown=65532:65532 /app/.next/standalone ./
COPY --from=build --chown=65532:65532 /app/.next/static ./.next/static
COPY --from=build --chown=65532:65532 /app/public ./public
USER nonroot
EXPOSE 3000
CMD ["server.js"]
