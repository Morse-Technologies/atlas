# Atlas app

Frontend of Atlas (Next.js 16, React 19, TypeScript strict, Tailwind 4, pnpm).

```bash
pnpm install
pnpm dev        # http://localhost:3000, mock data, no backend needed
pnpm lint
pnpm typecheck
pnpm test         # vitest: HTTP adapter, key conversion, SSE reader
pnpm gen:api      # refresh src/lib/api/schema.d.ts from ../server/api/openapi.yaml
pnpm build
```

- Every screen talks to `AtlasApi` (`src/lib/api/types.ts`). `pnpm dev` uses demo data (`mock.ts`); with `NEXT_PUBLIC_ATLAS_API=live` it uses the Go server through `http.ts`, same origin (`/api/v1`). `contract.ts` makes `tsc` fail when the types stop matching `../server/api/openapi.yaml`: run `pnpm gen:api` after the contract changes.
- Demo sign-in: `admin@admin.es` / `admin123` / code `123456`. `ATLAS_AUTH_OFF=1` disables the route gate for screenshots.
- `NEXT_PUBLIC_ATLAS_EDITION=pro` shows the Pro screens.
- Product context: `PRODUCT.md`. Visual system: `DESIGN.md`. Architecture: `../docs/`.
