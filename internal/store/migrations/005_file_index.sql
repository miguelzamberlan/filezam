-- Índice de nomes para a pesquisa: uma linha por entrada (caminho base-relativo), reconstruído
-- em varreduras periódicas (gen) e ajustado pelas operações do próprio app.
CREATE TABLE file_index (
  path TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  name TEXT NOT NULL,
  name_lc TEXT NOT NULL,
  type TEXT NOT NULL,
  size INTEGER NOT NULL,
  mtime INTEGER NOT NULL,
  gen INTEGER NOT NULL
);
CREATE INDEX file_index_name ON file_index(name_lc);
CREATE INDEX file_index_parent ON file_index(parent);
CREATE TABLE index_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  last_full_at INTEGER,
  entries INTEGER NOT NULL DEFAULT 0,
  gen INTEGER NOT NULL DEFAULT 0
);
INSERT INTO index_state(id, last_full_at, entries, gen) VALUES (1, NULL, 0, 0);
