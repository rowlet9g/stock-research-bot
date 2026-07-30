ALTER TABLE theses
ADD COLUMN allocation_category TEXT NOT NULL DEFAULT '';

ALTER TABLE theses
ADD COLUMN protected_quantity_units INTEGER NOT NULL DEFAULT 0;

ALTER TABLE theses
ADD COLUMN increase_condition TEXT NOT NULL DEFAULT '';
