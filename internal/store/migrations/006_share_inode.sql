-- O link público passa a lembrar o inode (e o dispositivo) do item compartilhado: um item
-- novo criado no mesmo caminho não reativa um link antigo.
ALTER TABLE shares ADD COLUMN dev INTEGER NOT NULL DEFAULT 0;
ALTER TABLE shares ADD COLUMN ino INTEGER NOT NULL DEFAULT 0;
