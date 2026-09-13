-- O que um job em andamento precisa desfazer se o processo cair no meio. A linha nasce antes de
-- o job tocar o disco e some quando ele termina (bem ou mal): sobra só a de um job que um
-- reinício interrompeu, e a manutenção a consome.
--   mode 'temps': apagar os temporários de cópia com a marca do job em path (e abaixo dele);
--                 o que já terminou fica.
--   mode 'tree':  apagar path inteiro (pasta de extração, .zip temporário).
CREATE TABLE job_cleanup (
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  path TEXT NOT NULL, -- base-relativo
  mode TEXT NOT NULL,
  PRIMARY KEY (job_id, path)
);
