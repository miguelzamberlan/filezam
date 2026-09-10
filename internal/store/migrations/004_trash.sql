-- Lixeira: cada item excluído vai para <trash_dir>/<id>/<name> e ganha uma linha aqui
-- com o caminho original (base-relativo) para permitir restaurar.
CREATE TABLE trash (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  trash_dir TEXT NOT NULL,
  name TEXT NOT NULL,
  path TEXT NOT NULL,
  type TEXT NOT NULL,
  size INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER NOT NULL
);
CREATE INDEX trash_deleted ON trash(deleted_at);
CREATE INDEX trash_path ON trash(path);
