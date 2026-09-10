-- Verificação em duas etapas (TOTP). O segredo fica cifrado (AES-GCM com a chave do servidor);
-- totp_counter guarda o último intervalo aceito (anti-replay); totp_recovery é um JSON com os
-- hashes dos códigos de recuperação ainda não usados.
ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_enabled_at INTEGER;
ALTER TABLE users ADD COLUMN totp_counter INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_recovery TEXT NOT NULL DEFAULT '';
