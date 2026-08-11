#!/bin/sh
set -eu

SOURCE_DIR=${1:-/tmp/nexgram-security}
GRAMSRV_ENV=/etc/gramsrv/gramsrv.env
BACKUP_DIR=/var/backups/nexgram-security
STAMP=$(date -u +%Y%m%dT%H%M%SZ)

require_file() {
    if [ ! -f "$SOURCE_DIR/$1" ]; then
        echo "Missing deployment file: $SOURCE_DIR/$1" >&2
        exit 1
    fi
}

set_env() {
    key=$1
    value=$2
    if grep -q "^${key}=" "$GRAMSRV_ENV"; then
        sed -i "s|^${key}=.*|${key}=${value}|" "$GRAMSRV_ENV"
    else
        printf '\n%s=%s\n' "$key" "$value" >>"$GRAMSRV_ENV"
    fi
}

for file in \
    nexgram-flood-guard.nft \
    nexgram-flood-guard-apply.sh \
    nexgram-flood-guard.sysctl \
    nexgram-flood-guard.service \
    nexgram-flood-monitor.py \
    nexgram-flood-monitor.service \
    gramsrv-flood-guard.conf
do
    require_file "$file"
done

# Validate everything before changing the active firewall or service config.
/usr/sbin/nft -c -f "$SOURCE_DIR/nexgram-flood-guard.nft"
/bin/sh -n "$SOURCE_DIR/nexgram-flood-guard-apply.sh"
/usr/bin/python3 -c "compile(open('$SOURCE_DIR/nexgram-flood-monitor.py', encoding='utf-8').read(), '$SOURCE_DIR/nexgram-flood-monitor.py', 'exec')"

install -d -m 0750 /etc/nexgram /usr/local/lib/nexgram "$BACKUP_DIR"
install -m 0640 "$SOURCE_DIR/nexgram-flood-guard.nft" /etc/nexgram/flood-guard.nft
install -m 0750 "$SOURCE_DIR/nexgram-flood-guard-apply.sh" /usr/local/sbin/nexgram-flood-guard-apply
install -m 0644 "$SOURCE_DIR/nexgram-flood-guard.sysctl" /etc/sysctl.d/90-nexgram-flood-guard.conf
install -m 0644 "$SOURCE_DIR/nexgram-flood-guard.service" /etc/systemd/system/nexgram-flood-guard.service
install -m 0750 "$SOURCE_DIR/nexgram-flood-monitor.py" /usr/local/lib/nexgram/flood-monitor.py
install -m 0644 "$SOURCE_DIR/nexgram-flood-monitor.service" /etc/systemd/system/nexgram-flood-monitor.service
install -d -m 0755 /etc/systemd/system/gramsrv.service.d
install -m 0644 "$SOURCE_DIR/gramsrv-flood-guard.conf" /etc/systemd/system/gramsrv.service.d/flood-guard.conf

cp -a "$GRAMSRV_ENV" "$BACKUP_DIR/gramsrv.env.$STAMP"
set_env TELESRV_MTPROTO_MAX_CONNECTIONS 8192
set_env TELESRV_MTPROTO_MAX_CONNECTIONS_PER_IP 256
set_env TELESRV_MTPROTO_MAX_CONCURRENT_HANDSHAKES 128

/usr/sbin/sysctl -p /etc/sysctl.d/90-nexgram-flood-guard.conf
/usr/bin/systemctl daemon-reload
/usr/bin/systemd-analyze verify /etc/systemd/system/nexgram-flood-guard.service /etc/systemd/system/nexgram-flood-monitor.service
/usr/bin/systemctl enable --now nexgram-flood-guard.service
/usr/bin/systemctl enable --now nexgram-flood-monitor.service

# systemd sends SIGTERM and grants the existing service its configured 30-second
# graceful-stop window before starting it with the new admission limits.
/usr/bin/systemctl restart gramsrv.service

/usr/bin/systemctl is-active --quiet nexgram-flood-guard.service
/usr/bin/systemctl is-active --quiet nexgram-flood-monitor.service
/usr/bin/systemctl is-active --quiet gramsrv.service

echo "NexGram flood protection installed successfully."
