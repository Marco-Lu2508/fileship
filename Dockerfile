# Stage 1: Build Frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Backend
FROM golang:1.23-alpine AS backend-builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY . .
# Copy built frontend AFTER COPY . . so it's available for go:embed
COPY --from=frontend-builder /app/frontend/dist ./internal/static/dist
RUN CGO_ENABLED=1 GOOS=linux go build -mod=vendor -ldflags="-s -w" -o fileship ./cmd/fileship

# Stage 3: Final minimal image
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /app/fileship /usr/local/bin/fileship
RUN mkdir -p /data

EXPOSE 8080
ENV ROOT_PATH=/data
ENV DB_PATH=/app/fileship.db

VOLUME ["/data"]

CMD ["/usr/local/bin/fileship"]
