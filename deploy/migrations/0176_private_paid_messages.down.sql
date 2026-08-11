ALTER TABLE scheduled_messages
    DROP CONSTRAINT scheduled_messages_allow_paid_stars_nonnegative;

ALTER TABLE scheduled_messages
    DROP COLUMN allow_paid_stars;

ALTER TABLE message_boxes
    DROP CONSTRAINT IF EXISTS message_boxes_paid_message_stars_nonnegative;

ALTER TABLE message_boxes
    DROP COLUMN IF EXISTS paid_message_stars;

ALTER TABLE private_messages
    DROP CONSTRAINT IF EXISTS private_messages_paid_message_stars_nonnegative;

ALTER TABLE private_messages
    DROP COLUMN IF EXISTS paid_message_stars;
