# syntax=docker/dockerfile:1
FROM node:22-bookworm-slim@sha256:4b150b48b4116e2a22f2c39cc27025fbf4f153e2078fd3b14d159fdb5c7279da AS build
WORKDIR /src
RUN npm install --global pnpm@11.3.0
COPY . .
RUN pnpm install --frozen-lockfile
# Next.js records rewrites at build time; this is a nonsecret service address.
ARG API_INTERNAL_ORIGIN=http://api:8080
ENV API_INTERNAL_ORIGIN=$API_INTERNAL_ORIGIN \
    NEXT_TELEMETRY_DISABLED=1
RUN pnpm generate:api \
    && pnpm build:web \
    && mkdir -p frontend/public

FROM node:22-bookworm-slim@sha256:4b150b48b4116e2a22f2c39cc27025fbf4f153e2078fd3b14d159fdb5c7279da AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    PORT=3000
COPY --from=build --chown=node:node /src/frontend/.next/standalone ./
COPY --from=build --chown=node:node /src/frontend/.next/static ./frontend/.next/static
COPY --from=build --chown=node:node /src/frontend/public ./frontend/public
RUN mkdir -p /app/frontend/.next/cache && chown -R node:node /app/frontend/.next/cache
USER node
EXPOSE 3000
HEALTHCHECK --interval=10s --timeout=5s --start-period=20s --retries=5 \
    CMD node -e "fetch('http://127.0.0.1:3000',{signal:AbortSignal.timeout(3000)}).then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "frontend/server.js"]
