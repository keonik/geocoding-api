FROM postgres:17

# Install PostGIS and pgvector extensions
RUN apt-get update && apt-get install -y \
  postgresql-17-postgis-3 \
  postgresql-17-pgvector \
  && rm -rf /var/lib/apt/lists/*

# Enable extensions and configure PostgreSQL for better performance with large updates
COPY <<EOF /docker-entrypoint-initdb.d/init-extensions.sql
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS vector;
EOF

# Increase max_wal_size for better performance during large migrations
# Default is 1GB, increasing to 4GB reduces checkpoint frequency
RUN echo "max_wal_size = 4GB" >> /usr/share/postgresql/postgresql.conf.sample
RUN echo "checkpoint_completion_target = 0.9" >> /usr/share/postgresql/postgresql.conf.sample