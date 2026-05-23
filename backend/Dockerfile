FROM node:24-bookworm-slim AS build

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates g++ make python3 \
  && rm -rf /var/lib/apt/lists/*

COPY package.json package-lock.json ./
RUN npm ci

COPY tsconfig.json vite.config.ts ./
COPY src ./src
COPY app ./app
RUN npm run build \
  && npm prune --omit=dev

FROM node:24-bookworm-slim

WORKDIR /app

ENV NODE_ENV=production \
  YOLOBOX_BACKEND_LISTEN=0.0.0.0:8787 \
  YOLOBOX_BACKEND_STATE=/data/backend.json \
  YOLOBOX_BACKEND_AUTH_DB=/data/auth.sqlite

RUN mkdir -p /data \
  && chown node:node /data

COPY --from=build --chown=node:node /app/package.json /app/package-lock.json ./
COPY --from=build --chown=node:node /app/node_modules ./node_modules
COPY --from=build --chown=node:node /app/dist ./dist
COPY --from=build --chown=node:node /app/dist-app ./dist-app

USER node

EXPOSE 8787
VOLUME ["/data"]

CMD ["npm", "start"]
