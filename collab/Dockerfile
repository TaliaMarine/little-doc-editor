FROM node:24-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:24-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /src/package.json ./
COPY --from=build /src/dist ./dist
RUN npm ci --omit=dev
USER node
ENTRYPOINT ["node", "dist/server.js"]
LABEL org.opencontainers.image.source="https://github.com/TaliaMarine/little-doc-editor"
