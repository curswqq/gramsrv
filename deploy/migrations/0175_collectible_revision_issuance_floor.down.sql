-- Data repair is intentionally irreversible: decreasing issued would make
-- already allocated collectible numbers eligible for duplicate issuance.
SELECT 1;
