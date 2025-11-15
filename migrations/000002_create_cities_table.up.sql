-- Create cities table for US city data
CREATE TABLE IF NOT EXISTS cities (
    id BIGSERIAL PRIMARY KEY,
    city VARCHAR(255) NOT NULL,
    city_ascii VARCHAR(255) NOT NULL,
    state_id VARCHAR(2) NOT NULL,
    state_name VARCHAR(255) NOT NULL,
    county_fips VARCHAR(10),
    county_name VARCHAR(255),
    lat DECIMAL(10, 7) NOT NULL,
    lng DECIMAL(11, 7) NOT NULL,
    population INTEGER,
    density DECIMAL(10, 2),
    source VARCHAR(50),
    military BOOLEAN DEFAULT FALSE,
    incorporated BOOLEAN DEFAULT FALSE,
    timezone VARCHAR(100),
    ranking INTEGER,
    zips TEXT,
    external_id VARCHAR(50),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_city_state UNIQUE (city_ascii, state_id)
);

-- Create indexes for efficient lookups
CREATE INDEX idx_cities_city ON cities (city);
CREATE INDEX idx_cities_city_ascii ON cities (city_ascii);
CREATE INDEX idx_cities_state_id ON cities (state_id);
CREATE INDEX idx_cities_state_name ON cities (state_name);
CREATE INDEX idx_cities_county_name ON cities (county_name);
CREATE INDEX idx_cities_ranking ON cities (ranking);

-- Create trigram indexes for fuzzy searching
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX idx_cities_city_trgm ON cities USING gin (city gin_trgm_ops);
CREATE INDEX idx_cities_city_ascii_trgm ON cities USING gin (city_ascii gin_trgm_ops);

-- Create spatial index for location-based queries
CREATE INDEX idx_cities_location ON cities USING btree (lat, lng);
