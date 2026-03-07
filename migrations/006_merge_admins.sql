-- Step 1: add is_admin column
ALTER TABLE responders ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;

-- Step 2: backfill pin_hash and mark is_admin for existing admins
UPDATE responders r
SET is_admin = TRUE,
    pin_hash  = a.pin_hash
FROM admins a
WHERE r.phone_number = a.phone_number;

-- Step 3: insert any admins not yet in responders
INSERT INTO responders (phone_number, is_admin, pin_hash)
SELECT a.phone_number, TRUE, a.pin_hash
FROM admins a
WHERE NOT EXISTS (
    SELECT 1 FROM responders r WHERE r.phone_number = a.phone_number
);

-- Step 4: drop the admins table
DROP TABLE admins;
