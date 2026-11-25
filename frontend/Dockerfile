FROM --platform=$BUILDPLATFORM node:26-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npx vite build

FROM nginxinc/nginx-unprivileged:1.31-alpine
USER root
RUN apk upgrade --no-cache
USER 101
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build --chown=101:101 /src/dist /usr/share/nginx/html
COPY --chmod=755 docker/40-lde-config.sh /docker-entrypoint.d/40-lde-config.sh
EXPOSE 8080
LABEL org.opencontainers.image.source="https://github.com/TaliaMarine/little-doc-editor"
