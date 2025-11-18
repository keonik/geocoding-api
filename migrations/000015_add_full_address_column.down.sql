-- Drop trigger and function
DROP TRIGGER IF EXISTS ohio_addresses_full_address_trigger ON ohio_addresses;
DROP FUNCTION IF EXISTS update_full_address();

-- Drop indexes
DROP INDEX IF EXISTS idx_ohio_addresses_full_address_trgm;
DROP INDEX IF EXISTS idx_ohio_addresses_full_address;

-- Drop column
ALTER TABLE ohio_addresses DROP COLUMN IF EXISTS full_address;
