#!/usr/bin/env python3
"""Monitor the NexGram nftables guard and notify configured bot owners."""

from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys
import time
from typing import Any
from urllib import parse, request


INTERVAL = max(5, int(os.environ.get("NEXGRAM_GUARD_INTERVAL", "10")))
ALERT_COOLDOWN = max(60, int(os.environ.get("NEXGRAM_GUARD_ALERT_COOLDOWN", "300")))
RECOVERY_AFTER = max(30, int(os.environ.get("NEXGRAM_GUARD_RECOVERY_AFTER", "120")))
STATE_PATH = Path(os.environ.get("NEXGRAM_GUARD_STATE", "/var/lib/nexgram-flood-guard/state.json"))
BOT_TOKEN = os.environ.get("BOT_TOKEN", "").strip()
OWNER_IDS = tuple(
    owner.strip()
    for owner in os.environ.get("OWNER_IDS", "").split(",")
    if owner.strip().isdigit()
)
PRODUCT_NAME = os.environ.get("PRODUCT_NAME", "NexGram").strip() or "NexGram"

NFT_COUNTERS = (
    "blocked_ipv4",
    "blocked_ipv6",
    "syn_global_drop",
    "syn_source_drop_ipv4",
    "syn_source_drop_ipv6",
    "syn_subnet_drop_ipv4",
    "syn_subnet_drop_ipv6",
    "syn_accepted",
)


def load_state() -> dict[str, Any]:
    try:
        value = json.loads(STATE_PATH.read_text(encoding="utf-8"))
        return value if isinstance(value, dict) else {}
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {}


def save_state(state: dict[str, Any]) -> None:
    STATE_PATH.parent.mkdir(parents=True, exist_ok=True)
    temporary = STATE_PATH.with_suffix(".tmp")
    temporary.write_text(json.dumps(state, separators=(",", ":")), encoding="utf-8")
    os.replace(temporary, STATE_PATH)


def nft_counters() -> dict[str, int]:
    completed = subprocess.run(
        ["/usr/sbin/nft", "-j", "list", "table", "inet", "nexgram_guard"],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        timeout=5,
    )
    document = json.loads(completed.stdout)
    counters: dict[str, int] = {}
    for item in document.get("nftables", []):
        counter = item.get("counter") if isinstance(item, dict) else None
        if not isinstance(counter, dict):
            continue
        name = str(counter.get("name", ""))
        if name in NFT_COUNTERS:
            counters[name] = int(counter.get("packets", 0))
    return {name: counters.get(name, 0) for name in NFT_COUNTERS}


def protocol_counters() -> dict[str, int]:
    result: dict[str, int] = {}
    lines = Path("/proc/net/netstat").read_text(encoding="ascii").splitlines()
    for index in range(0, len(lines) - 1, 2):
        header = lines[index].split()
        values = lines[index + 1].split()
        if not header or not values or header[0] != values[0]:
            continue
        for key, value in zip(header[1:], values[1:]):
            if key in {"SyncookiesSent", "SyncookiesFailed", "TCPReqQFullDoCookies", "ListenDrops"}:
                result[key] = int(value)

    lines = Path("/proc/net/snmp").read_text(encoding="ascii").splitlines()
    for index in range(0, len(lines) - 1, 2):
        header = lines[index].split()
        values = lines[index + 1].split()
        if not header or not values or header[0] != "Tcp:" or values[0] != "Tcp:":
            continue
        for key, value in zip(header[1:], values[1:]):
            if key in {"PassiveOpens", "EstabResets"}:
                result[key] = int(value)
    return result


def deltas(current: dict[str, int], previous: dict[str, Any]) -> dict[str, int]:
    result: dict[str, int] = {}
    for name, value in current.items():
        old = int(previous.get(name, value))
        result[name] = max(0, value - old)
    return result


def send_message(text: str) -> None:
    if not BOT_TOKEN or not OWNER_IDS:
        raise RuntimeError("BOT_TOKEN and OWNER_IDS are required for security notifications")
    endpoint = f"https://api.telegram.org/bot{BOT_TOKEN}/sendMessage"
    delivered = 0
    for owner_id in OWNER_IDS:
        try:
            payload = parse.urlencode({"chat_id": owner_id, "text": text}).encode("utf-8")
            with request.urlopen(endpoint, data=payload, timeout=10) as response:
                body = json.loads(response.read().decode("utf-8"))
                if not body.get("ok"):
                    raise RuntimeError("Bot API rejected the message")
            delivered += 1
        except Exception as error:
            print(f"security notification delivery failed for one owner: {error}", flush=True)
    if delivered == 0:
        raise RuntimeError("security notification could not be delivered to any owner")


def attack_reasons(delta: dict[str, int]) -> list[str]:
    blocked = sum(delta.get(name, 0) for name in NFT_COUNTERS if "drop" in name or name.startswith("blocked_"))
    reasons: list[str] = []
    if blocked >= 25:
        reasons.append(f"заблокировано пакетов: {blocked}")
    if delta.get("SyncookiesFailed", 0) >= 100:
        reasons.append(f"ошибочных SYN cookies: {delta['SyncookiesFailed']}")
    if delta.get("SyncookiesSent", 0) >= 50:
        reasons.append(f"отправлено SYN cookies: {delta['SyncookiesSent']}")
    if delta.get("ListenDrops", 0) >= 10:
        reasons.append(f"переполнений очереди: {delta['ListenDrops']}")
    if delta.get("PassiveOpens", 0) >= 500:
        reasons.append(f"новых TCP-соединений: {delta['PassiveOpens']}")
    if delta.get("EstabResets", 0) >= 500:
        reasons.append(f"сброшенных TCP-соединений: {delta['EstabResets']}")
    return reasons


def main() -> None:
    state = load_state()
    while True:
        try:
            current = {**nft_counters(), **protocol_counters()}
            previous = state.get("counters", {})
            delta = deltas(current, previous)
            now = int(time.time())
            reasons = attack_reasons(delta) if previous else []
            active = bool(state.get("active", False))

            if reasons:
                state["last_suspicious"] = now
                last_alert = int(state.get("last_alert", 0))
                if not active or now - last_alert >= ALERT_COOLDOWN:
                    send_message(
                        f"🛡 Обнаружена сетевая атака на {PRODUCT_NAME}\n\n"
                        f"Тип: TCP SYN/connection flood\n"
                        f"Порты: 2398, 2400\n"
                        + "\n".join(f"• {reason}" for reason in reasons)
                        + "\n\nДействие: вредоносный трафик ограничен, активные источники временно заблокированы."
                    )
                    print("security alert sent:", "; ".join(reasons), flush=True)
                    state["last_alert"] = now
                state["active"] = True
            elif active and now - int(state.get("last_suspicious", now)) >= RECOVERY_AFTER:
                send_message(
                    f"✅ Сетевая атака на {PRODUCT_NAME} прекратилась\n\n"
                    "Защитные ограничения остаются активными, сервис работает в штатном режиме."
                )
                print("security recovery notification sent", flush=True)
                state["active"] = False
                state["last_recovery"] = now

            state["counters"] = current
            state["updated_at"] = now
            save_state(state)
        except Exception as error:  # Keep monitoring after transient nft/network failures.
            print(f"security monitor error: {error}", flush=True)
        time.sleep(INTERVAL)


if __name__ == "__main__":
    if "--test-notification" in sys.argv:
        send_message(
            f"🛡 Защита {PRODUCT_NAME} от сетевых атак включена\n\n"
            "Это тестовое уведомление. Мониторинг TCP SYN/connection flood работает штатно."
        )
        print("security test notification sent", flush=True)
    else:
        main()
