FROM golang:1.26-alpine AS backend-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/ \
    ./cmd/server ./cmd/bootstrap-admin ./cmd/bootstrap-platform-admin

FROM alpine:3.23 AS backend
RUN apk add --no-cache ca-certificates \
    && addgroup -S harness \
    && adduser -S -G harness harness \
    && mkdir -p /app/data \
    && chown harness:harness /app/data
WORKDIR /app
COPY --from=backend-build /out/ /usr/local/bin/
USER harness
CMD ["server"]

FROM node:24-alpine AS frontend-build
WORKDIR /src
RUN npm install --global pnpm@12.10.1
COPY pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json ./web/
RUN pnpm install --frozen-lockfile
COPY web ./web
RUN pnpm --filter harness-admin-web run build

FROM nginx:1.28-alpine AS frontend
COPY deploy/demo/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=frontend-build /src/web/dist/ /usr/share/nginx/html/
