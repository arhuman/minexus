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
curl -o minion "http://$NEXUS_SERVER:{{.WebPort}}/binaries/minion/$(uname -s)-$(uname -m)" || {
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


