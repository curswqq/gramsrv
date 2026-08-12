ALTER TABLE message_boxes
  ADD COLUMN outbox_read_date integer NOT NULL DEFAULT 0;

-- Preserve exact dates already present in the update journal. The message row
-- becomes authoritative after this migration, so later journal retention can
-- no longer erase a read receipt.
WITH historical_receipts AS (
  SELECT
    m.owner_user_id,
    m.box_id,
    MIN(e.date)::integer AS read_date
  FROM message_boxes m
  JOIN user_update_events e
    ON e.user_id = m.owner_user_id
   AND e.event_type = 'read_history_outbox'
   AND e.peer_type = m.peer_type
   AND e.peer_id = m.peer_id
   AND e.max_id >= m.box_id
  WHERE m.peer_type = 'user'
    AND m.outgoing
    AND NOT m.deleted
    AND m.message_date >= EXTRACT(EPOCH FROM now())::integer - 604800
    AND e.date > 0
  GROUP BY m.owner_user_id, m.box_id
)
UPDATE message_boxes m
SET outbox_read_date = r.read_date
FROM historical_receipts r
WHERE m.owner_user_id = r.owner_user_id
  AND m.box_id = r.box_id;
