# Glyph collab service (Hocuspocus). Build from the repository root — the
# service bundles the editor schema it shares with the frontend from src/lib:
#
#   docker build -f collab/Dockerfile -t glyph-collab .

# Stage 1: install and bundle
FROM node:22-alpine AS builder

RUN corepack enable && corepack prepare pnpm@10 --activate

WORKDIR /repo

COPY package.json pnpm-lock.yaml pnpm-workspace.yaml .npmrc ./
COPY collab/package.json collab/
# --ignore-scripts: the root package's `prepare` runs svelte-kit sync, which
# the collab bundle doesn't need.
RUN pnpm install --frozen-lockfile --ignore-scripts

COPY src/lib src/lib
COPY collab collab
RUN pnpm --filter @k5s/glyph-collab build

# Stage 2: run the single-file bundle
FROM node:22-alpine AS runner

RUN addgroup -g 1001 -S nodejs && adduser -S nodejs -u 1001 -G nodejs
WORKDIR /app
COPY --from=builder --chown=nodejs:nodejs /repo/collab/dist ./dist

USER nodejs
ENV NODE_ENV=production
ENV PORT=1235
EXPOSE 1235

HEALTHCHECK --interval=10s --timeout=3s --retries=6 \
  CMD wget -qO- http://127.0.0.1:1235/collab/healthz || exit 1

CMD ["node", "--enable-source-maps", "dist/server.js"]
