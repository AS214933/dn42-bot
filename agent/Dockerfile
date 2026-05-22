# Build stage for tcping
# NOTE: tcping upstream HEAD may bump Go requirements or break builds; pin to a stable ref.
FROM golang:1.26-alpine AS tcping-builder

ARG TCPING_REF=v2.8.0

RUN apk add --no-cache git bash make

# Build pouriyajamshidi/tcping from source
RUN set -e; \
    git clone --depth 1 --branch "${TCPING_REF}" https://github.com/pouriyajamshidi/tcping.git /tmp/tcping && \
    cd /tmp/tcping && \
    make build

# Python builder stage
FROM python:3.14-slim AS python-builder

WORKDIR /app

# Install build dependencies temporarily (need full build toolchain for aiohttp)
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Copy and install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt

# Final stage
FROM python:3.14-slim

# Build metadata
ARG BUILD_DATE
ARG GIT_COMMIT

# Install only runtime dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    iputils-ping \
    traceroute \
    wireguard-tools \
    iproute2 \
    bird2 \
    vnstat \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy tcping binary from builder stage
COPY --from=tcping-builder /tmp/tcping/target/tcping /usr/local/bin/tcping
RUN chmod +x /usr/local/bin/tcping

# Copy Python packages from builder
COPY --from=python-builder /install /usr/local

# Set working directory
WORKDIR /app

# Copy application code
COPY . .

# Write build info
RUN echo "${BUILD_DATE:-unknown}" > /app/build_date.txt && \
    echo "${GIT_COMMIT:-unknown}" > /app/git_commit.txt

# Precompile Python bytecode and remove unnecessary files
RUN python -m compileall -q /app && \
    find /app -type d -name "*.egg-info" -exec rm -rf {} + 2>/dev/null || true

# Expose the API port (default 54321)
EXPOSE 54321

# Run the agent
CMD ["python3", "main.py"]
