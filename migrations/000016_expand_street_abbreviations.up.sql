-- Migration 16: Expand street abbreviations in full_address column
-- This creates a function to expand common street type abbreviations
-- and updates the trigger to use it

-- Create function to expand street abbreviations
CREATE OR REPLACE FUNCTION expand_street_abbreviation(street_name TEXT) RETURNS TEXT AS $$
DECLARE
    expanded TEXT;
BEGIN
    IF street_name IS NULL THEN
        RETURN NULL;
    END IF;
    
    expanded := street_name;
    
    -- Most common abbreviations (in order of frequency from database analysis)
    -- DR (765k) -> Drive
    expanded := REGEXP_REPLACE(expanded, '\s+DR$', ' Drive', 'i');
    
    -- RD (679k) -> Road
    expanded := REGEXP_REPLACE(expanded, '\s+RD$', ' Road', 'i');
    
    -- AVE/AV (486k + 118k) -> Avenue
    expanded := REGEXP_REPLACE(expanded, '\s+AVE$', ' Avenue', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+AV$', ' Avenue', 'i');
    
    -- ST (456k) -> Street
    expanded := REGEXP_REPLACE(expanded, '\s+ST$', ' Street', 'i');
    
    -- CT (200k) -> Court
    expanded := REGEXP_REPLACE(expanded, '\s+CT$', ' Court', 'i');
    
    -- LN (185k) -> Lane
    expanded := REGEXP_REPLACE(expanded, '\s+LN$', ' Lane', 'i');
    
    -- BLVD (88k) -> Boulevard
    expanded := REGEXP_REPLACE(expanded, '\s+BLVD$', ' Boulevard', 'i');
    
    -- Directional abbreviations (NW, SW, NE, SE, N, S, E, W)
    expanded := REGEXP_REPLACE(expanded, '\s+NW$', ' Northwest', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+SW$', ' Southwest', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+NE$', ' Northeast', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+SE$', ' Southeast', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+N$', ' North', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+S$', ' South', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+E$', ' East', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+W$', ' West', 'i');
    
    -- WAY (63k) -> Way (already full)
    -- PL (60k) -> Place
    expanded := REGEXP_REPLACE(expanded, '\s+PL$', ' Place', 'i');
    
    -- CIR (57k) -> Circle
    expanded := REGEXP_REPLACE(expanded, '\s+CIR$', ' Circle', 'i');
    
    -- TRL (25k) -> Trail
    expanded := REGEXP_REPLACE(expanded, '\s+TRL$', ' Trail', 'i');
    
    -- PKWY (14k) -> Parkway
    expanded := REGEXP_REPLACE(expanded, '\s+PKWY$', ' Parkway', 'i');
    
    -- TER (6k) -> Terrace
    expanded := REGEXP_REPLACE(expanded, '\s+TER$', ' Terrace', 'i');
    
    -- WY (7k) -> Way
    expanded := REGEXP_REPLACE(expanded, '\s+WY$', ' Way', 'i');
    
    -- Additional common abbreviations
    expanded := REGEXP_REPLACE(expanded, '\s+HWY$', ' Highway', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+PIKE$', ' Pike', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+ALY$', ' Alley', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+ANX$', ' Annex', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+EXPY$', ' Expressway', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+EXT$', ' Extension', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+FWY$', ' Freeway', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+GRV$', ' Grove', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+HTS$', ' Heights', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+JCT$', ' Junction', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+LNDG$', ' Landing', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+LOOP$', ' Loop', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+PT$', ' Point', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+SQ$', ' Square', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+TRCE$', ' Trace', 'i');
    expanded := REGEXP_REPLACE(expanded, '\s+VW$', ' View', 'i');
    
    RETURN expanded;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Drop the old trigger and function
DROP TRIGGER IF EXISTS update_full_address_trigger ON ohio_addresses;
DROP FUNCTION IF EXISTS update_full_address() CASCADE;

-- Create new trigger function with abbreviation expansion
CREATE OR REPLACE FUNCTION update_full_address() RETURNS TRIGGER AS $$
BEGIN
    NEW.full_address := CONCAT_WS(', ',
        NULLIF(CONCAT_WS(' ',
            NULLIF(NEW.house_number, ''),
            NULLIF(expand_street_abbreviation(NEW.street), ''),
            NULLIF(NEW.unit, '')
        ), ''),
        NULLIF(NEW.city, ''),
        CASE 
            WHEN NEW.region IS NOT NULL AND NEW.region != '' 
            THEN NEW.region 
            ELSE 'OH' 
        END,
        NULLIF(NEW.postcode, '')
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Recreate the trigger
CREATE TRIGGER update_full_address_trigger
    BEFORE INSERT OR UPDATE ON ohio_addresses
    FOR EACH ROW
    EXECUTE FUNCTION update_full_address();

-- Update existing records to use expanded abbreviations
UPDATE ohio_addresses 
SET full_address = CONCAT_WS(', ',
    NULLIF(CONCAT_WS(' ',
        NULLIF(house_number, ''),
        NULLIF(expand_street_abbreviation(street), ''),
        NULLIF(unit, '')
    ), ''),
    NULLIF(city, ''),
    CASE 
        WHEN region IS NOT NULL AND region != '' 
        THEN region 
        ELSE 'OH' 
    END,
    NULLIF(postcode, '')
)
WHERE street IS NOT NULL;
