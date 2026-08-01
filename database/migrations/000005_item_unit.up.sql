ALTER TABLE items
    ADD COLUMN unit TEXT NOT NULL DEFAULT 'amount'
        CHECK (unit IN ('amount', 'g', 'kg', 'ml', 'l', 'pack', 'bottle', 'can', 'jar', 'cup', 'bunch', 'bag'));
