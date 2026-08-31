import { Ban, CheckCircle2, Gavel, Loader2, RefreshCw, Undo2, User, X } from "lucide-react";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { Alert, Badge, EmptyRow } from "../components/ui";
import { useI18n } from "../i18n";
import { formatDate } from "../lib/format";
import type { StarGiftAuctionBidRow, StarGiftAuctionRow } from "../types";
import { LottiePreview } from "./GiftsPage";

export function AuctionBidsModal({
  auction,
  onClose,
}: {
  auction: StarGiftAuctionRow;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [bids, setBids] = useState<StarGiftAuctionBidRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [cancellingBid, setCancellingBid] = useState<StarGiftAuctionBidRow | null>(null);
  const [refundStars, setRefundStars] = useState(true);

  const loadBids = () => {
    setLoading(true);
    api.auctionBids(auction.GiftID)
      .then((res) => {
        setBids(res.Bids || []);
        setError("");
      })
      .catch((err) => setError(errorMessage(err)))
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    loadBids();
  }, [auction.GiftID]);

  return createPortal(
    <div className="modal-backdrop" role="presentation">
      <section
        className="modal command-modal"
        role="dialog"
        aria-modal="true"
        aria-label={t("auctions.bidsTitle")}
        style={{ maxWidth: "900px", width: "95vw" }}
      >
        <div className="modal-head">
          <div style={{ display: "flex", alignItems: "center", gap: "1rem" }}>
            <LottiePreview giftID={auction.GiftID} revision={1} compact />
            <div>
              <div className="eyebrow">{t("layout.auctions")} / {auction.Slug}</div>
              <h2>{auction.GiftTitle || `Gift #${auction.GiftID}`}</h2>
              <p className="bot-create-note">{t("auctions.bidsSubtitle")}</p>
            </div>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
            <button className="btn compact" type="button" onClick={loadBids} disabled={loading}>
              <RefreshCw size={14} className={loading ? "spin" : ""} />
            </button>
            <button className="icon-btn" type="button" onClick={onClose} aria-label={t("common.close")}>
              <X size={15} />
            </button>
          </div>
        </div>

        <div className="command-body">
          {error ? <Alert>{error}</Alert> : null}

          <div
            style={{
              display: "flex",
              flexWrap: "wrap",
              gap: "1rem",
              padding: "0.75rem 1rem",
              background: "var(--bg-subtle, rgba(255,255,255,0.03))",
              borderRadius: "8px",
              marginBottom: "1rem",
              fontSize: "0.875rem",
            }}
          >
            <div>
              <span className="dim">{t("common.status")}:</span>{" "}
              <Badge tone={auction.Status === "active" ? "good" : "neutral"}>
                {t(`auctions.status_${auction.Status}`)}
              </Badge>
            </div>
            <div>
              <span className="dim">{t("auctions.round")}:</span>{" "}
              <strong>{auction.CurrentRound} / {auction.TotalRounds}</strong>
            </div>
            <div>
              <span className="dim">{t("auctions.giftsPerRound")}:</span>{" "}
              <strong>{auction.GiftsPerRound}</strong>
            </div>
            <div>
              <span className="dim">{t("auctions.minBid")}:</span>{" "}
              <strong>⭐ {parseInt(auction.MinBidAmount || "0").toLocaleString()}</strong>
            </div>
            <div>
              <span className="dim">{t("auctions.activeBids")}:</span>{" "}
              <strong style={{ color: "var(--color-primary, #60a5fa)" }}>{auction.ActiveBids}</strong>
            </div>
          </div>

          <div className="table-wrap" style={{ maxHeight: "400px", overflowY: "auto" }}>
            <table className="data-table">
              <thead>
                <tr>
                  <th style={{ width: "40px" }}>#</th>
                  <th>{t("auctions.bidder")}</th>
                  <th>{t("auctions.amount")}</th>
                  <th>{t("auctions.bidDate")}</th>
                  <th>{t("common.status")}</th>
                  <th>{t("auctions.recipient")}</th>
                  <th>{t("auctions.message")}</th>
                  <th>{t("common.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {loading && bids.length === 0 ? (
                  <tr>
                    <td colSpan={8} style={{ textAlign: "center", padding: "2rem" }}>
                      <Loader2 size={24} className="spin" style={{ margin: "0 auto" }} />
                    </td>
                  </tr>
                ) : bids.length === 0 ? (
                  <EmptyRow colSpan={8} />
                ) : (
                  bids.map((bid, index) => {
                    const fullName = [bid.BidderFirstName, bid.BidderLastName].filter(Boolean).join(" ");
                    return (
                      <tr key={`${bid.GiftID}-${bid.BidderUserID}`}>
                        <td>
                          <strong>{index + 1}</strong>
                        </td>
                        <td>
                          <div>
                            <strong>
                              {bid.BidderUsername ? `@${bid.BidderUsername}` : fullName || `User #${bid.BidderUserID}`}
                            </strong>
                            <div className="dim small">ID: {bid.BidderUserID}</div>
                          </div>
                        </td>
                        <td>
                          <strong style={{ color: "var(--color-good, #4ade80)", fontSize: "1rem" }}>
                            ⭐ {parseInt(bid.Amount || "0").toLocaleString()}
                          </strong>
                        </td>
                        <td>
                          <div className="small">
                            {bid.BidDate > 0 ? formatDate(new Date(bid.BidDate * 1000).toISOString()) : "—"}
                          </div>
                        </td>
                        <td>
                          {bid.Active ? (
                            <Badge tone="good">{t("auctions.bidStatusActive")}</Badge>
                          ) : bid.Returned ? (
                            <Badge tone="neutral">{t("auctions.bidStatusReturned")}</Badge>
                          ) : bid.AcquiredCount > 0 ? (
                            <Badge tone="good">{t("auctions.bidStatusWon")}</Badge>
                          ) : (
                            <Badge tone="danger">{t("auctions.bidStatusInactive")}</Badge>
                          )}
                        </td>
                        <td>
                          <div className="small">
                            <span className="dim">{bid.RecipientPeerType}:</span> {bid.RecipientPeerID}
                          </div>
                        </td>
                        <td>
                          {bid.Message ? (
                            <div className="small">
                              "{bid.Message}" {bid.HideName ? <span className="dim">({t("auctions.anonymous")})</span> : null}
                            </div>
                          ) : bid.HideName ? (
                            <span className="dim small">{t("auctions.anonymous")}</span>
                          ) : (
                            <span className="dim">—</span>
                          )}
                        </td>
                        <td>
                          {bid.Active ? (
                            <button
                              className="btn compact danger"
                              type="button"
                              onClick={() => {
                                setCancellingBid(bid);
                                setRefundStars(true);
                              }}
                            >
                              <Ban size={13} />
                              <span>{t("auctions.cancelBid")}</span>
                            </button>
                          ) : (
                            <span className="dim">—</span>
                          )}
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>
        </div>

        {cancellingBid ? (
          <div
            style={{
              padding: "1rem",
              margin: "1rem",
              background: "var(--bg-card, rgba(239, 68, 68, 0.08))",
              border: "1px solid var(--border-danger, rgba(239, 68, 68, 0.3))",
              borderRadius: "8px",
            }}
          >
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start" }}>
              <div>
                <strong style={{ color: "var(--color-danger, #ef4444)" }}>
                  {t("auctions.cancelBidConfirm")}
                </strong>
                <p className="small dim" style={{ marginTop: "0.25rem" }}>
                  {t("auctions.bidder")}: <strong>{cancellingBid.BidderUsername ? `@${cancellingBid.BidderUsername}` : cancellingBid.BidderUserID}</strong> |{" "}
                  {t("auctions.amount")}: <strong>⭐ {parseInt(cancellingBid.Amount || "0").toLocaleString()}</strong>
                </p>
              </div>
              <button
                className="icon-btn"
                type="button"
                onClick={() => setCancellingBid(null)}
                aria-label={t("common.cancel")}
              >
                <X size={14} />
              </button>
            </div>

            <div style={{ marginTop: "0.75rem" }}>
              <label style={{ display: "flex", alignItems: "center", gap: "0.5rem", cursor: "pointer" }}>
                <input
                  type="checkbox"
                  checked={refundStars}
                  onChange={(e) => setRefundStars(e.target.checked)}
                />
                <span><strong>{t("auctions.refundStars")}</strong> (⭐ {parseInt(cancellingBid.Amount || "0").toLocaleString()})</span>
              </label>
              <div className="small dim" style={{ marginLeft: "1.5rem" }}>
                {t("auctions.refundStarsHint")}
              </div>
            </div>

            <div style={{ marginTop: "1rem", display: "flex", gap: "0.5rem", justifyContent: "flex-end" }}>
              <button className="btn" type="button" onClick={() => setCancellingBid(null)}>
                {t("common.cancel")}
              </button>
              <ActionButton
                compact
                label={t("auctions.cancelBid")}
                icon={<Ban size={14} />}
                tone="danger"
                path="/api/actions/cancel-star-gift-auction-bid"
                payload={() => ({
                  gift_id: cancellingBid.GiftID,
                  bidder_user_id: cancellingBid.BidderUserID,
                  refund_stars: refundStars,
                })}
                onDone={() => {
                  setCancellingBid(null);
                  loadBids();
                }}
              />
            </div>
          </div>
        ) : null}
      </section>
    </div>,
    document.body
  );
}