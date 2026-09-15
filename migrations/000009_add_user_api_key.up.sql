ALTER TABLE users ADD COLUMN api_key TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_api_key ON users(api_key) WHERE api_key IS NOT NULL AND api_key <> '';
