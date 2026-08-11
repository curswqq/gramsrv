#!/bin/sh
set -eu

CONFIG=${NEXGRAM_FLOOD_GUARD_CONFIG:-/etc/nexgram/flood-guard.nft}

/usr/sbin/nft delete table inet nexgram_guard 2>/dev/null || true
/usr/sbin/nft -f "$CONFIG"
