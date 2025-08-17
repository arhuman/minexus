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
ARCH_RAW=$(uname -m)
echo "DEBUG: Raw architecture from uname -m: $ARCH_RAW"

# Translate architecture names to match server expectations
case "$ARCH_RAW" in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64|arm64)
        ARCH="arm64"
        ;;
    *)
        ARCH="$ARCH_RAW"
        ;;
esac

echo "DEBUG: Translated architecture: $ARCH"
DOWNLOAD_URL="http://$NEXUS_SERVER:{{.WebPort}}/download/minion/${OS}-${ARCH}"
echo "DEBUG: Download URL: $DOWNLOAD_URL"

curl -o minion "$DOWNLOAD_URL" || {
    echo "Failed to download minion binary"
    # Show first 200 chars of response to see if it's HTML
    echo "DEBUG: Response content (first 200 chars):"
    head -c 200 minion 2>/dev/null
    exit 1
}

# Verify it's a binary, not HTML
file_type=$(file -b minion 2>/dev/null || echo "unknown")
echo "DEBUG: Downloaded file type: $file_type"
if [[ "$file_type" == *"HTML"* ]] || [[ "$file_type" == *"text"* ]]; then
    echo "ERROR: Downloaded file appears to be HTML/text, not a binary!"
    echo "First 500 characters of file:"
    head -c 500 minion
    exit 1
fi

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

