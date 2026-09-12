-- Configurações globais editáveis pelo administrador pela interface. Antes desta migração
-- tudo vinha de variável de ambiente lida na subida; os tetos dos links de envio precisam
-- mudar sem reiniciar o serviço.
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

-- Apelido escolhido pelo usuário no lugar do token ('' = link só por token). O índice é
-- parcial porque a esmagadora maioria das linhas fica com '' e elas precisam coexistir.
ALTER TABLE shares ADD COLUMN slug TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX shares_slug ON shares(slug) WHERE slug <> '';

-- Link de envio: escrita anônima numa pasta vazia, com cota e teto de arquivos obrigatórios.
ALTER TABLE shares ADD COLUMN mode TEXT NOT NULL DEFAULT 'read';
ALTER TABLE shares ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE shares ADD COLUMN max_file_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE shares ADD COLUMN max_files INTEGER NOT NULL DEFAULT 0;

-- Recibo de cada arquivo recebido por um link de envio: é a fonte única da cota consumida,
-- da contagem de arquivos e da lista que o visitante vê dos próprios envios.
--
-- Dois nomes, de propósito. `name` é o nome FINAL no disco, que o dono vê e que a auditoria
-- registra; `sent_name` é o que o remetente pediu, e é só ele que volta para o remetente. Um
-- link de envio não deixa ler a pasta, então contar que "seu arquivo virou nome (1).ext" o
-- transformaria num oráculo: bastaria enviar 1 byte com um nome chutado para descobrir se
-- alguém já mandou aquele arquivo, ou o que o dono guarda ali.
CREATE TABLE share_uploads (
  id INTEGER PRIMARY KEY,
  share_id INTEGER NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
  sender TEXT NOT NULL,
  name TEXT NOT NULL,
  sent_name TEXT NOT NULL,
  size INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE INDEX share_uploads_sender ON share_uploads(share_id, sender);

-- As sessões em blocos abertas de um link de envio contam na cota dele (elas pré-alocam o
-- tamanho declarado no disco), e precisam ser invisíveis no caminho autenticado do dono.
ALTER TABLE uploads ADD COLUMN share_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE uploads ADD COLUMN sender TEXT NOT NULL DEFAULT '';
-- Nome que o remetente pediu, quando ele difere do nome final (ver share_uploads).
ALTER TABLE uploads ADD COLUMN sent_name TEXT NOT NULL DEFAULT '';
CREATE INDEX uploads_share ON uploads(share_id);
