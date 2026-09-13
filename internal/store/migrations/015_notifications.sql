-- Notificações por usuário. Genérica de propósito: `kind` diz o que aconteceu e `data` (JSON)
-- guarda os números e nomes para a interface montar o texto no idioma de quem lê. `group_key`
-- junta eventos em série numa notificação só enquanto ela não foi lida: vinte arquivos chegando
-- por um link viram "20 arquivos", não vinte avisos.
CREATE TABLE notifications (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  group_key TEXT NOT NULL DEFAULT '',
  data TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  read_at INTEGER
);
CREATE INDEX notifications_user ON notifications(user_id, updated_at);
CREATE UNIQUE INDEX notifications_open_group ON notifications(user_id, group_key) WHERE read_at IS NULL AND group_key <> '';
