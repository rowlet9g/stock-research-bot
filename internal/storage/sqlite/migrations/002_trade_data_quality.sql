ALTER TABLE trades
    ADD COLUMN price_source TEXT NOT NULL DEFAULT 'reported'
    CHECK (price_source IN ('reported', 'derived_amount_div_quantity'));

ALTER TABLE trades
    ADD COLUMN taxes_known INTEGER NOT NULL DEFAULT 1
    CHECK (taxes_known IN (0, 1));

ALTER TABLE trades
    ADD COLUMN time_precision TEXT NOT NULL DEFAULT 'day'
    CHECK (time_precision IN ('day', 'second'));
