# Stage 1: Build Go binary
FROM golang:1.25 AS builder

# Install git for module fetching
RUN apt-get update && apt-get install -y git ca-certificates && rm -rf /var/lib/apt/lists/*

# Set workdir
WORKDIR /app

# Copy Go modules manifests
COPY go.mod ./
RUN go mod download

# Copy the rest of the app
COPY . .

# Build statically-linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o scraper

# Stage 2: Final image with Chromium
FROM debian:bullseye-slim

# # Install necessary packages for headless Chromium
# RUN apt-get update && apt-get install -y \
#     ca-certificates \
#     fonts-liberation \
#     libasound2 \
#     libatk1.0-0 \
#     libatk-bridge2.0-0 \
#     libcups2 \
#     libdbus-1-3 \
#     libdrm2 \
#     libx11-xcb1 \
#     libxcomposite1 \
#     libxdamage1 \
#     libxext6 \
#     libxfixes3 \
#     libxrandr2 \
#     libxrender1 \
#     libgbm1 \
#     libpango-1.0-0 \
#     libpangocairo-1.0-0 \
#     libgtk-3-0 \
#     wget \
#     unzip \
#     && rm -rf /var/lib/apt/lists/*

# Install Chromium (stable version)
RUN apt-get update && apt-get install -y chromium \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/scraper .

# Set environment variables for headless Chromium
ENV CHROME_PATH=/usr/bin/chromium
ENV CHROMEDP_HEADLESS=1
ENV CHROMEDP_NO_SANDBOX=1

# Expose port if needed (optional)
# EXPOSE 8080

# Command
CMD ["./scraper"]
