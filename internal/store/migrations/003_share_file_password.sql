-- Links públicos de arquivo único (kind='file') e senha opcional (Argon2id em password_hash).
ALTER TABLE shares ADD COLUMN kind TEXT NOT NULL DEFAULT 'dir';
ALTER TABLE shares ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
