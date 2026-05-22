FROM python:3.14-slim

# Build metadata
ARG BUILD_DATE
ARG GIT_COMMIT

# Install system dependencies required by the server
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    iputils-ping \
    traceroute \
    dnsutils \
    whois \
    graphviz \
    build-essential \
    git \
    gpg \
    gpg-agent \
    dirmngr \
    openssh-client \
    nodejs \
    npm \
    && rm -rf /var/lib/apt/lists/*

# Note: tcping should be installed separately. 
# You can install it from https://github.com/nodeseeker/tcping or mount it as a volume

# Set working directory
WORKDIR /app

# Copy requirements first for better caching
COPY requirements.txt .

# Install Python dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Copy application code
COPY . .

# Write build info
RUN echo "${BUILD_DATE:-unknown}" > /app/build_date.txt && \
    echo "${GIT_COMMIT:-unknown}" > /app/git_commit.txt

# Precompile Python bytecode to avoid compile on first run
RUN python -m compileall -q /app

# Expose the webhook port (default 3443)
EXPOSE 3443

# Run the server
CMD ["python3", "main.py"]
