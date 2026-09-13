-- A pesquisa passa a ignorar acentos, cedilha e maiúsculas: a coluna guarda o nome dobrado
-- (vfs.Fold), não só em minúsculas. index_state.fold é a versão da dobra das linhas; o Open
-- redobra o índice quando ela fica atrás de vfs.FoldVersion (0 = só minúsculas, da 005).
ALTER TABLE file_index RENAME COLUMN name_lc TO name_fold;
ALTER TABLE index_state ADD COLUMN fold INTEGER NOT NULL DEFAULT 0;
