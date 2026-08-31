import { Ban, CheckCircle2, Clock, Gavel, List, Loader2, Plus, RefreshCw, Trophy } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { Alert, Badge, EmptyRow, Metric, PageFrame } from "../components/ui";
import { useI18n } from "../i18n";
import { formatDate } from "../lib/format";
import type { StarGiftAuctionRow } from "../types";
import { AuctionBidsModal } from "./AuctionBidsModal";
import { CreateAuctionModal } from "./CreateAuctionModal";
import { LottiePreview } from "./GiftsPage";

export function AuctionsPage() {
  const { t } = useI18n();
  const [auctions, setAuctions] = useState<StarGiftAuctionRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [selectedAuctionForBids, setSelectedAuctionForBids] = useState<StarGiftAuctionRow | null>(null);
  const [filter, setFilter] = useState<"all" | "active" | "pending" | "completed" | "cancelled">("all");

  const load = () => {
    setLoading(true);
    api.auctions()
      .then((res) => {
        setAuctions(res.Auctions || []);
        setError("");
      })
      .catch((err) => setError(errorMessage(err)))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    load();
    const timer = setInterval(load, 30000);
    return () => clearInterval(timer);
  }, []);

  const filtered = useMemo(() => {
    if (filter === "all") return auctions;
    return auctions.filter((a) => a.Status === filter);
  }, [auctions, filter]);

  const activeCount = auctions.filter((a) => a.Status === "active").length;
  const totalVolume = auctions.reduce((acc, a) => acc + (parseInt(a.TotalVolume) || 0), 0);
  const totalWinners = auctions.reduce((acc, a) => acc + (a.WinnersCount || 0), 0);

  const formatCountdown = (timestamp: number) => {
    const diff = timestamp - Math.floor(Date.now() / 1000);
    if (diff <= 0) return t("auctions.roundEnding");
    const m = Math.floor(diff / 60);
    const s = diff % 60;
    if (m >= 60) {
      const h = Math.floor(m / 60);
      return `${h}h ${m % 60}m`;
    }
    return `${m}m ${s}s`;
  };

  return (
    <PageFrame
      title={t("layout.auctions")}
      eyebrow={t("layout.gifts")}
      actions={
        <div className="button-group">
          <button className="btn" type="button" onClick={load} disabled={loading}>
            <RefreshCw size={15} className={loading ? "spin" : ""} />
            <span>{t("common.refresh")}</span>
          </button>
          <button className="btn primary" type="button" onClick={() => setShowCreateModal(true)}>
            <Plus size={15} />
            <span>{t("auctions.create")}</span>
          </button>
        </div>
      }
    >
      {error ? <Alert>{error}</Alert> : null}

      <div className="metric-row gift-metrics">
        <Metric label={t("auctions.totalAuctions")} value={String(auctions.length)} />
        <Metric label={t("auctions.activeAuctions")} value={String(activeCount)} tone="good" />
        <Metric label={t("auctions.totalVolume")} value={`⭐ ${totalVolume.toLocaleString()}`} />
        <Metric label={t("auctions.totalWinners")} value={String(totalWinners)} />
      </div>

      <div style={{ marginTop: "1rem", marginBottom: "1rem", display: "flex", gap: "0.5rem" }}>
        {(["all", "active", "pending", "completed", "cancelled"] as const).map((s) => (
          <button
            key={s}
            className={`btn ${filter === s ? "primary" : ""}`}
            type="button"
            onClick={() => setFilter(s)}
          >
            {t(`auctions.status_${s}`)}
          </button>
        ))}
      </div>

      <div className="table-wrap gift-table-wrap">
        <table className="data-table gift-table">
          <thead>
            <tr>
              <th>{t("common.title")}</th>
              <th>{t("auctions.slug")}</th>
              <th>{t("common.status")}</th>
              <th>{t("auctions.round")}</th>
              <th>{t("auctions.nextRound")}</th>
              <th>{t("auctions.minBid")}</th>
              <th>{t("auctions.activeBids")}</th>
              <th>{t("auctions.winners")}</th>
              <th>{t("common.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 ? (
              <EmptyRow colSpan={9} />
            ) : (
              filtered.map((auction) => (
                <tr key={auction.GiftID}>
                  <td>
                    <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
                      <LottiePreview giftID={auction.GiftID} revision={1} compact />
                      <div>
                        <strong>{auction.GiftTitle || `Gift #${auction.GiftID}`}</strong>
                        <div className="dim small">ID: {auction.GiftID}</div>
                      </div>
                    </div>
                  </td>
                  <td>
                    <code>{auction.Slug}</code>
                  </td>
                  <td>
                    <Badge
                      tone={
                        auction.Status === "active"
                          ? "good"
                          : auction.Status === "pending"
                          ? "neutral"
                          : auction.Status === "completed"
                          ? "neutral"
                          : "danger"
                      }
                    >
                      {t(`auctions.status_${auction.Status}`)}
                    </Badge>
                  </td>
                  <td>
                    <strong>
                      {auction.CurrentRound} / {auction.TotalRounds}
                    </strong>
                    <div className="dim small">
                      {auction.GiftsPerRound} {t("auctions.perRound")}
                    </div>
                  </td>
                  <td>
                    {auction.Status === "active" ? (
                      <div>
                        <strong>{formatCountdown(auction.NextRoundAt)}</strong>
                        <div className="dim small">{formatDate(new Date(auction.NextRoundAt * 1000).toISOString())}</div>
                      </div>
                    ) : (
                      <span className="dim">—</span>
                    )}
                  </td>
                  <td>
                    <strong>⭐ {parseInt(auction.MinBidAmount || "0").toLocaleString()}</strong>
                  </td>
                  <td>
                    <strong>{auction.ActiveBids}</strong>
                    <div className="dim small">{auction.TotalBids} {t("auctions.total")}</div>
                  </td>
                  <td>
                    <strong>{auction.WinnersCount}</strong>
                    <div className="dim small">
                      {auction.GiftsLeft} {t("auctions.left")}
                    </div>
                  </td>
                  <td>
                    <div style={{ display: "flex", gap: "0.5rem", alignItems: "center" }}>
                      <button
                        className="btn compact"
                        type="button"
                        onClick={() => setSelectedAuctionForBids(auction)}
                      >
                        <List size={13} />
                        <span>{t("auctions.viewBids")}</span>
                      </button>
                      {auction.Status === "active" || auction.Status === "pending" ? (
                        <ActionButton
                          compact
                          label={t("auctions.cancel")}
                          icon={<Ban size={14} />}
                          tone="danger"
                          path="/api/actions/cancel-star-gift-auction"
                          payload={() => ({ gift_id: auction.GiftID })}
                          onDone={load}
                        />
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {selectedAuctionForBids ? (
        <AuctionBidsModal
          auction={selectedAuctionForBids}
          onClose={() => {
            setSelectedAuctionForBids(null);
            load();
          }}
        />
      ) : null}

      {showCreateModal ? (
        <CreateAuctionModal
          onClose={() => setShowCreateModal(false)}
          onCreated={() => {
            setShowCreateModal(false);
            load();
          }}
        />
      ) : null}
    </PageFrame>
  );
}
