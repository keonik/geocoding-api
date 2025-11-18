-- Add full_address column to ohio_addresses table
ALTER TABLE ohio_addresses ADD COLUMN IF NOT EXISTS full_address TEXT;

-- Create a generated column that concatenates all address parts
-- Format: "123 Main St, Unit 5, Columbus, OH 43215"
UPDATE ohio_addresses SET full_address = 
  CONCAT_WS(', ',
    NULLIF(CONCAT_WS(' ', 
      NULLIF(house_number, ''), 
      NULLIF(street, ''),
      CASE WHEN unit != '' THEN 'Unit ' || unit ELSE NULL END
    ), ''),
    NULLIF(city, ''),
    CONCAT_WS(' ', NULLIF(region, ''), NULLIF(postcode, ''))
  );

-- Create a GIN index for fast full-text search on the full address
CREATE INDEX IF NOT EXISTS idx_ohio_addresses_full_address_trgm ON ohio_addresses USING gin (full_address gin_trgm_ops);

-- Create a regular index for sorting/filtering
CREATE INDEX IF NOT EXISTS idx_ohio_addresses_full_address ON ohio_addresses(full_address);

-- Add a trigger to automatically update full_address when address components change
CREATE OR REPLACE FUNCTION update_full_address()
RETURNS TRIGGER AS $$
BEGIN
  NEW.full_address := CONCAT_WS(', ',
    NULLIF(CONCAT_WS(' ', 
      NULLIF(NEW.house_number, ''), 
      NULLIF(NEW.street, ''),
      CASE WHEN NEW.unit != '' THEN 'Unit ' || NEW.unit ELSE NULL END
    ), ''),
    NULLIF(NEW.city, ''),
    CONCAT_WS(' ', NULLIF(NEW.region, ''), NULLIF(NEW.postcode, ''))
  );
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ohio_addresses_full_address_trigger
  BEFORE INSERT OR UPDATE ON ohio_addresses
  FOR EACH ROW
  EXECUTE FUNCTION update_full_address();
