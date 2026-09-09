-- O token do link público passa a ser guardado também em claro, para que a tela
-- "Compartilhados" possa copiar o link de novo. O hash continua sendo a chave de
-- consulta pública. Links criados antes desta migração ficam com token vazio.
ALTER TABLE shares ADD COLUMN token TEXT NOT NULL DEFAULT '';
