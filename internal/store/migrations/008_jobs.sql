-- Histórico de operações em segundo plano (copiar/mover/excluir). O progresso ao vivo continua
-- em memória; aqui fica o registro para sobreviver a reinícios e alimentar a tela "Operações".
CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  label TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  done INTEGER NOT NULL DEFAULT 0,
  total INTEGER NOT NULL DEFAULT 0,
  bytes_done INTEGER NOT NULL DEFAULT 0,
  bytes_total INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  warnings INTEGER NOT NULL DEFAULT 0,
  started_at INTEGER NOT NULL,
  finished_at INTEGER
);
CREATE INDEX jobs_user_started ON jobs(user_id, started_at);
