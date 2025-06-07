-- Database schema creation for SQLite

CREATE TABLE IF NOT EXISTS identities (
    address TEXT PRIMARY KEY,
    state TEXT NOT NULL,
    stake REAL NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance improvement
CREATE INDEX IF NOT EXISTS idx_state ON identities(state);
CREATE INDEX IF NOT EXISTS idx_stake ON identities(stake);
CREATE INDEX IF NOT EXISTS idx_eligible ON identities(state, stake);
CREATE INDEX IF NOT EXISTS idx_timestamp ON identities(timestamp);
CREATE INDEX IF NOT EXISTS idx_updated_at ON identities(updated_at);

-- View for eligible identities
CREATE VIEW IF NOT EXISTS eligible_identities AS
SELECT address, state, stake, updated_at
FROM identities 
WHERE state IN ('Human', 'Verified', 'Newbie') 
  AND stake >= 10000;

-- Trigger to automatically update updated_at
CREATE TRIGGER IF NOT EXISTS update_timestamp 
    AFTER UPDATE ON identities
BEGIN
    UPDATE identities SET updated_at = CURRENT_TIMESTAMP 
    WHERE address = NEW.address;
END;