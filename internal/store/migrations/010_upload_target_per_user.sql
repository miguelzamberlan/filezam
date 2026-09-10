-- Sessão chunked exclusiva por (usuário, destino), não mais por destino: uma sessão parada
-- não bloqueia outro usuário do mesmo escopo por 24 h. Quem finaliza primeiro grava; o outro
-- recebe o conflito normal do Finalize (409 exists, ou substitui se pediu overwrite).
DROP INDEX uploads_target;
CREATE UNIQUE INDEX uploads_target ON uploads(user_id, dir, name);
