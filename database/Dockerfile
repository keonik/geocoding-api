FROM postgres:17

# Install PostGIS and pgvector extensions
RUN apt-get update && apt-get install -y \
  postgresql-17-postgis-3 \
  postgresql-17-pgvector \
  && rm -rf /var/lib/apt/lists/*

# Enable extensions
COPY <<EOF /docker-entrypoint-initdb.d/init-extensions.sql
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS vector;
EOF