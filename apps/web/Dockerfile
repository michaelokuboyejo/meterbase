# Build context is the REPO ROOT (see docker-compose.yaml).
# Use Debian-based images; Alpine (musl) lacks lightningcss native binaries for arm64.
FROM node:22-slim AS base
ENV PNPM_HOME="/pnpm"
ENV PATH="/pnpm:$PATH"
RUN corepack enable

FROM base AS build
WORKDIR /repo
COPY . .
# Use --frozen-lockfile in CI once pnpm-lock.yaml is committed.
RUN pnpm install --no-frozen-lockfile
RUN pnpm --filter web build

FROM base AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
# Next.js standalone output (monorepo layout puts the server under apps/web).
COPY --from=build /repo/apps/web/.next/standalone ./
COPY --from=build /repo/apps/web/.next/static ./apps/web/.next/static
COPY --from=build /repo/apps/web/public ./apps/web/public
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
