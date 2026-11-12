CREATE TABLE assistant_records (
    key TEXT PRIMARY KEY,
    document TEXT NOT NULL,
    updated_at INTEGER NOT NULL DEFAULT (CAST(strftime('%s','now') AS INTEGER))
);
