-- Link cujo item sumiu, mudou de tipo ou foi substituído por fora (Samba, SSH). A manutenção grava
-- quando viu isso pela primeira vez, limpa se o item voltar e revoga o link depois de um prazo
-- de carência: um disco que demora a montar num reinício não pode levar os links embora.
ALTER TABLE shares ADD COLUMN broken_since INTEGER;
