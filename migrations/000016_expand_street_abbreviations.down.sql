-- Rollback Migration 16: Remove street abbreviation expansion

-- Drop the trigger
DROP TRIGGER IF EXISTS update_full_address_trigger ON ohio_addresses;

-- Drop the new trigger function
DROP FUNCTION IF EXISTS update_full_address();

-- Drop the abbreviation expansion function
DROP FUNCTION IF EXISTS expand_street_abbreviation(TEXT);

-- Restore original trigger function (without expansion)
CREATE OR REPLACE FUNCTION update_full_address() RETURNS TRIGGER AS $$
BEGIN
    NEW.full_address := CONCAT_WS(', ',
        NULLIF(CONCAT_WS(' ',
            NULLIF(NEW.house_number, ''),
            NULLIF(NEW.street, ''),
            NULLIF(NEW.unit, '')
        ), ''),
        NULLIF(NEW.city, ''),
        CASE 
            WHEN NEW.state IS NOT NULL AND NEW.state != '' 
            THEN NEW.state 
            ELSE 'OH' 
        END,
        NULLIF(NEW.postcode, '')
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Recreate the original trigger
CREATE TRIGGER update_full_address_trigger
    BEFORE INSERT OR UPDATE ON ohio_addresses
    FOR EACH ROW
    EXECUTE FUNCTION update_full_address();

-- Restore full_address to use original abbreviated street names
UPDATE ohio_addresses 
SET full_address = CONCAT_WS(', ',
    NULLIF(CONCAT_WS(' ',
        NULLIF(house_number, ''),
        NULLIF(street, ''),
        NULLIF(unit, '')
    ), ''),
    NULLIF(city, ''),
    CASE 
        WHEN state IS NOT NULL AND state != '' 
        THEN state 
        ELSE 'OH' 
    END,
    NULLIF(postcode, '')
)
WHERE street IS NOT NULL;
