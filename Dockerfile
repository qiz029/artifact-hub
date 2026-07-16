FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.23-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /artifact-hub ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 artifact
WORKDIR /app
COPY --from=backend /artifact-hub /app/artifact-hub
COPY --from=frontend /src/frontend/dist /app/frontend/dist
USER artifact
ENV HTTP_ADDR=:8080 FRONTEND_DIR=/app/frontend/dist
EXPOSE 8080
ENTRYPOINT ["/app/artifact-hub"]
