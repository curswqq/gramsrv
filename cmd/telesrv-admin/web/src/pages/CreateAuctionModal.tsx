import { Gavel, X } from "lucide-react";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api";
import { ActionButton } from "../components/ActionButton";
import { useI18n } from "../i18n";
import { toInt } from "../lib/format";
import type { StarGiftRow } from "../types";

export function CreateAuctionModal({
  initialGiftID,
  onClose,
  onCreated
}: {
  initialGiftID?: string;
  onClose: () => void;
  onCreated: () => void;
}) {
  const { t } = useI18n();
  const [gifts, setGifts] = useState<StarGiftRow[]>([]);
  const [selectedGiftID, setSelectedGiftID] = useState(initialGiftID || "");
  const [slug, setSlug] = useState("");
  const [giftsPerRound, setGiftsPerRound] = useState("1");
  const [roundDuration, setRoundDuration] = useState("3600");
  const [totalRounds, setTotalRounds] = useState("10");
  const [minBid, setMinBid] = useState("500");

  useEffect(() => {
    api.gifts().then((res) => {
      setGifts(res.Gifts || []);
      if (!selectedGiftID && res.Gifts && res.Gifts.length > 0) {
        setSelectedGiftID(res.Gifts[0].GiftID);
      }
    }).catch(() => {});
  }, []);

  const cleanSlug = slug.trim().toLowerCase().replace(/[^a-z0-9_-]/g, "");
  const valid =
    toInt(selectedGiftID) > 0 &&
    cleanSlug.length > 0 &&
    toInt(giftsPerRound) >= 1 &&
    toInt(roundDuration) >= 60 &&
    toInt(totalRounds) >= 1 &&
    toInt(minBid) >= 1;

  return createPortal(
    <div className="modal-backdrop" role="presentation">
      <section className="modal command-modal" role="dialog" aria-modal="true" aria-label={t("auctions.createTitle")}>
        <div className="modal-head">
          <div>
            <div className="eyebrow">{t("layout.auctions")}</div>
            <h2>{t("auctions.createTitle")}</h2>
            <p className="bot-create-note">{t("auctions.createHint")}</p>
          </div>
          <button className="icon-btn" type="button" onClick={onClose} aria-label={t("common.close")}><X size={15} /></button>
        </div>
        <div className="command-body">
          <div className="bot-create-fields">
            <label className="duration-field">
              <span>{t("auctions.selectGift")}</span>
              <select value={selectedGiftID} onChange={(e) => setSelectedGiftID(e.target.value)}>
                {gifts.map((g) => (
                  <option key={g.GiftID} value={g.GiftID}>
                    {g.Title} (ID: {g.GiftID}, ⭐ {g.Stars})
                  </option>
                ))}
              </select>
            </label>
            <label className="duration-field">
              <span>{t("auctions.slug")}</span>
              <input
                value={slug}
                onChange={(e) => setSlug(e.target.value)}
                placeholder="gift-auction-1"
                maxLength={32}
              />
            </label>
            <label className="duration-field">
              <span>{t("auctions.minBid")}</span>
              <input
                value={minBid}
                onChange={(e) => setMinBid(e.target.value)}
                type="number"
                min="1"
                placeholder="500"
              />
            </label>
            <label className="duration-field">
              <span>{t("auctions.giftsPerRound")}</span>
              <input
                value={giftsPerRound}
                onChange={(e) => setGiftsPerRound(e.target.value)}
                type="number"
                min="1"
                placeholder="1"
              />
            </label>
            <label className="duration-field">
              <span>{t("auctions.totalRounds")}</span>
              <input
                value={totalRounds}
                onChange={(e) => setTotalRounds(e.target.value)}
                type="number"
                min="1"
                placeholder="10"
              />
            </label>
            <label className="duration-field">
              <span>{t("auctions.roundDuration")}</span>
              <select value={roundDuration} onChange={(e) => setRoundDuration(e.target.value)}>
                <option value="300">5 min</option>
                <option value="600">10 min</option>
                <option value="900">15 min</option>
                <option value="1800">30 min</option>
                <option value="3600">1 hour</option>
                <option value="7200">2 hours</option>
                <option value="86400">24 hours</option>
              </select>
            </label>
          </div>
          <p className="bot-create-note">
            Total Supply: {toInt(giftsPerRound) * toInt(totalRounds)} gifts across {totalRounds} rounds
          </p>
        </div>
        <div className="modal-actions">
          <button className="btn" type="button" onClick={onClose}>{t("common.close")}</button>
          <ActionButton
            label={t("auctions.createButton")}
            icon={<Gavel size={15} />}
            tone="neutral"
            disabled={!valid}
            path="/api/actions/create-star-gift-auction"
            payload={() => ({
              gift_id: selectedGiftID,
              slug: cleanSlug,
              gifts_per_round: toInt(giftsPerRound),
              round_duration: toInt(roundDuration),
              total_rounds: toInt(totalRounds),
              min_bid_amount: minBid,
              start_date: 0
            })}
            onDone={onCreated}
          />
        </div>
      </section>
    </div>,
    document.body
  );
}
