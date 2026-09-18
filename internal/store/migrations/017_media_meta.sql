-- Cache dos dados técnicos de foto e vídeo. Ler o cabeçalho de um arquivo custa uma abertura e
-- alguns saltos no disco; numa pasta com milhares de vídeos isso é o trabalho todo, e repeti-lo a
-- cada análise deixaria a tela inútil. A chave de invalidação é o par tamanho + mtime: o arquivo
-- mudou, a linha vale nada e o cabeçalho é lido de novo.
--
-- `meta` guarda o retrato inteiro em JSON, do mesmo jeito que `notifications.data`: o que dá para
-- ler de um contêiner cresce com o tempo, e um campo novo não pode virar uma migração nova.
-- `readable = 0` é cache negativo — o arquivo prometia mídia pela extensão e o cabeçalho não abriu.
CREATE TABLE media_meta (
  path TEXT PRIMARY KEY,
  size INTEGER NOT NULL,
  mtime INTEGER NOT NULL,
  readable INTEGER NOT NULL DEFAULT 1,
  meta TEXT NOT NULL DEFAULT '',
  read_at INTEGER NOT NULL
);
