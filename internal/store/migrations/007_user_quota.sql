-- Cota de disco por usuário em bytes (0 = sem limite). Medida sobre o escopo do usuário.
ALTER TABLE users ADD COLUMN quota INTEGER NOT NULL DEFAULT 0;
