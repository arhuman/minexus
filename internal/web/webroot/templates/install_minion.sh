#!/bin/bash
# Minion Installation Script
# Generated on: {{.Date}}
# Server: {{.ServerURL}}

set -e

# Configuration
NEXUS_SERVER="{{.ServerURL}}"
MINION_PORT="{{.MinionPort}}"
MINION_ID="${MINION_ID:-{{.MinionID}}}"

echo "Installing Minion client..."
echo "Nexus Server: $NEXUS_SERVER"
echo "Minion Port: $MINION_PORT"
echo "Minion ID: $MINION_ID"

# Download and install minion binary
echo "Downloading minion binary..."
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Map architecture names to expected format
case "$ARCH" in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
    armv7l|armv7)
        echo "Error: 32-bit ARM is not supported"
        exit 1
        ;;
    *)
        echo "Error: Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Handle special cases for OS names
case "$OS" in
    darwin)
        # macOS is already correct
        ;;
    linux)
        # Linux is already correct
        ;;
    mingw*|msys*|cygwin*)
        OS="windows"
        PLATFORM="${OS}-${ARCH}.exe"
        ;;
    *)
        echo "Error: Unsupported operating system: $OS"
        exit 1
        ;;
esac

# Build platform string
if [ -z "$PLATFORM" ]; then
    PLATFORM="${OS}-${ARCH}"
fi

echo "Detected platform: $PLATFORM"
curl -o minion "http://$NEXUS_SERVER:{{.WebPort}}/download/minion/${PLATFORM}" || {
    echo "Failed to download minion binary"
    exit 1
}

chmod +x minion

# Create systemd service or run directly
if [ "$1" = "--systemd" ]; then
    cat > /etc/systemd/system/minion.service <<EOF
[Unit]
Description=Minexus Minion
After=network.target

[Service]
Type=simple
User=nobody
Environment="NEXUS_SERVER=$NEXUS_SERVER"
Environment="NEXUS_MINION_PORT=$MINION_PORT"
Environment="MINION_ID=$MINION_ID"
ExecStart=$(pwd)/minion
Restart=always

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable minion
    systemctl start minion
    echo "Minion installed as systemd service"
else
    echo "Starting minion..."
    NEXUS_SERVER="$NEXUS_SERVER" NEXUS_MINION_PORT="$MINION_PORT" MINION_ID="$MINION_ID" ./minion
fi
