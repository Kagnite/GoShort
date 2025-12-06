DROP TABLE IF EXISTS urls;

-- Create URLs table
CREATE TABLE urls (
    id VARCHAR(36) PRIMARY KEY,
    original_url TEXT NOT NULL,
    short_code VARCHAR(50) UNIQUE NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    click_count BIGINT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_ip VARCHAR(45),
    user_agent VARCHAR(500),
    
    -- Indexes
    CONSTRAINT short_code_format CHECK (short_code ~ '^[a-zA-Z0-9_-]+$')
);

-- Create indexes for better performance
CREATE INDEX idx_urls_short_code ON urls(short_code) WHERE is_active = TRUE;
CREATE INDEX idx_urls_created_at ON urls(created_at DESC);
CREATE INDEX idx_urls_expires_at ON urls(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX idx_urls_created_by_ip ON urls(created_by_ip);

-- Add comments
COMMENT ON TABLE urls IS 'Stores URL shortening mappings';
COMMENT ON COLUMN urls.id IS 'Unique identifier (UUID)';
COMMENT ON COLUMN urls.original_url IS 'Original long URL';
COMMENT ON COLUMN urls.short_code IS 'Short code for the URL';