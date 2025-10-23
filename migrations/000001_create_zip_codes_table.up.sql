CREATE TABLE IF NOT EXISTS zip_codes (
    zip_code VARCHAR(10) PRIMARY KEY,
    city_name VARCHAR(255) NOT NULL,
    state_code VARCHAR(2) NOT NULL,
    state_name VARCHAR(255) NOT NULL,
    zcta BOOLEAN NOT NULL DEFAULT FALSE,
    zcta_parent VARCHAR(10),
    population DECIMAL(12,2),
    density DECIMAL(10,2),
    primary_county_code VARCHAR(10) NOT NULL,
    primary_county_name VARCHAR(255) NOT NULL,
    county_weights JSONB,
    county_names TEXT,
    county_codes TEXT,
    imprecise BOOLEAN NOT NULL DEFAULT FALSE,
    military BOOLEAN NOT NULL DEFAULT FALSE,
    timezone VARCHAR(100) NOT NULL,
    latitude DECIMAL(10,7) NOT NULL,
    longitude DECIMAL(10,7) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for better query performance
CREATE INDEX IF NOT EXISTS idx_zip_codes_state_code ON zip_codes(state_code);
CREATE INDEX IF NOT EXISTS idx_zip_codes_city_name ON zip_codes(city_name);
CREATE INDEX IF NOT EXISTS idx_zip_codes_state_name ON zip_codes(state_name);
CREATE INDEX IF NOT EXISTS idx_zip_codes_county_name ON zip_codes(primary_county_name);
CREATE INDEX IF NOT EXISTS idx_zip_codes_location ON zip_codes(latitude, longitude);