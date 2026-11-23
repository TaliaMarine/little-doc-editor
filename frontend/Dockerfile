FROM node:26-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npx vite build

FROM nginxinc/nginx-unprivileged:1.31-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 8080
LABEL org.opencontainers.image.source="https://github.com/TaliaMarine/little-doc-editor"
