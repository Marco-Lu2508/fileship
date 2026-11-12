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
    ca-certificates ffmpeg libsqlite3-0 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /app/target/release/fileship /usr/local/bin/fileship

ENV PORT=8080 \
    DB_PATH=/data/fileship.db \
    ROOT_PATH=/data

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["fileship"]
