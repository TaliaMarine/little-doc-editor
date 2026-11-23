FROM node:26-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build
RUN npm ci --omit=dev --ignore-scripts

FROM node:26-alpine
RUN apk upgrade --no-cache \
 && rm -rf /usr/local/lib/node_modules/npm /usr/local/bin/npm /usr/local/bin/npx /opt/yarn* /usr/local/bin/yarn*
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /src/package.json ./
COPY --from=build /src/node_modules ./node_modules
COPY --from=build /src/dist ./dist
USER node
ENTRYPOINT ["node", "dist/server.js"]
LABEL org.opencontainers.image.source="https://github.com/TaliaMarine/little-doc-editor"
