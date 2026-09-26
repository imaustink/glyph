-- Email lookups (sharing, adding org members, token subjects) match
-- case-insensitively on lower(email). Deliberately not UNIQUE: existing data
-- may hold the same address on several accounts (different IdPs, or
-- differing only in case); the store refuses to resolve such an address
-- instead of picking one.
CREATE INDEX IF NOT EXISTS users_email_lower_idx ON users (lower(email));
