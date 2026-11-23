FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/api /api
COPY --from=build /out/migrate /migrate
COPY db/migrations /db/migrations
USER nonroot:nonroot
ENTRYPOINT ["/api"]
LABEL org.opencontainers.image.source="https://github.com/TaliaMarine/little-doc-editor"
