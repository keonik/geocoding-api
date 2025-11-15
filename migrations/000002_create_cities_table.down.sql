-- Drop cities table and indexes
DROP INDEX IF EXISTS idx_cities_location;
DROP INDEX IF EXISTS idx_cities_city_ascii_trgm;
DROP INDEX IF EXISTS idx_cities_city_trgm;
DROP INDEX IF EXISTS idx_cities_ranking;
DROP INDEX IF EXISTS idx_cities_county_name;
DROP INDEX IF EXISTS idx_cities_state_name;
DROP INDEX IF EXISTS idx_cities_state_id;
DROP INDEX IF EXISTS idx_cities_city_ascii;
DROP INDEX IF EXISTS idx_cities_city;
DROP TABLE IF EXISTS cities;
