# NexGram TCP flood protection

This deployment bundle protects the public MTProto (`2398/tcp`) and TURN TCP
(`2400/tcp`) listeners without modifying UFW's iptables-nft-managed tables.

It provides four independent controls:

1. an early nftables prerouting guard with global, per-address and per-subnet SYN
   rate limits plus temporary 15-minute source blocking;
2. conservative kernel SYN backlog and conntrack sizing;
3. application admission caps configured in `/etc/gramsrv/gramsrv.env`;
4. a watchdog that reports attacks and recovery through the existing bot to all
   Telegram IDs in `OWNER_IDS`.

`gramsrv-flood-guard.conf` makes the firewall guard an ordering dependency of
the MTProto service so it is restored before the public listener after boot or
an explicit service start.

Recommended production application limits for the current host:

```dotenv
TELESRV_MTPROTO_MAX_CONNECTIONS=8192
TELESRV_MTPROTO_MAX_CONNECTIONS_PER_IP=256
TELESRV_MTPROTO_MAX_CONCURRENT_HANDSHAKES=128
```

The nftables service owns only `table inet nexgram_guard`. Reloading or stopping
it never flushes the full ruleset and therefore does not alter UFW, SSH, Caddy,
Docker, MTProto data, PostgreSQL data or bot data.

The production installer validates nftables, shell and Python syntax before it
changes the host. It backs up only `gramsrv.env`, installs the guard and monitor,
then restarts `gramsrv` through systemd's normal graceful-stop path. It does not
restart or modify the bot database.
