-- Exclusão para a lixeira em duas fases. A linha nasce com pending=1 antes de o item sair do
-- lugar e vira 0 quando ele chegou inteiro à lixeira. Uma linha pendente de um processo que caiu é
-- retomada pela manutenção: conclui o movimento (ou a remoção da origem, numa cópia entre
-- dispositivos) em vez de deixar uma linha sem item ou uma cópia parcial à vista na lixeira.
ALTER TABLE trash ADD COLUMN pending INTEGER NOT NULL DEFAULT 0;
CREATE INDEX trash_pending ON trash(pending) WHERE pending = 1;
