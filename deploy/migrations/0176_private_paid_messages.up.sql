ALTER TABLE private_messages
    ADD COLUMN paid_message_stars bigint NOT NULL DEFAULT 0;

ALTER TABLE private_messages
    ADD CONSTRAINT private_messages_paid_message_stars_nonnegative
    CHECK (paid_message_stars >= 0);

ALTER TABLE message_boxes
    ADD COLUMN paid_message_stars bigint NOT NULL DEFAULT 0;

ALTER TABLE message_boxes
    ADD CONSTRAINT message_boxes_paid_message_stars_nonnegative
    CHECK (paid_message_stars >= 0);

-- Scheduled delivery must retain the sender's authorization ceiling. The
-- recipient's current price is resolved again at dispatch time, so a price
-- increase above the approved ceiling fails safely instead of charging more.
ALTER TABLE scheduled_messages
    ADD COLUMN allow_paid_stars bigint NOT NULL DEFAULT 0;

ALTER TABLE scheduled_messages
    ADD CONSTRAINT scheduled_messages_allow_paid_stars_nonnegative
    CHECK (allow_paid_stars >= 0);
