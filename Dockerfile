# Fileship (Rust) — Production Image
# Multi-stage: Build mit LTO/fat-Profil, Runtime minimal (distroless-ish).

# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM rust:1.90-slim-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential pkg-config libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src ./src
COPY frontend/dist ./frontend/dist
RUN cargo test --release --all-targets && \
    cargo build --release && strip target/release/fileship

# ── Stage 2: Runtime ────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates ffmpeg libsqlite3-0 poppler-utils libheif-examples \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /app/target/release/fileship /usr/local/bin/fileship

ENV PORT=8080 \
    DB_PATH=/data/fileship.db \
    ROOT_PATH=/data

VOLUME ["/data"]
EXPOSE 8080/tcp 8080/udp
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD bash -c 'exec 3<>/dev/tcp/127.0.0.1/${PORT:-8080} && printf "GET /healthz HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n" >&3 && head -1 <&3 | grep -q " 200 "'
ENTRYPOINT ["fileship"]
