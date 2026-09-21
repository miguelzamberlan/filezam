// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

/** Abaixo disto não há velocidade a informar: é o rastro de uma média que já parou. */
const FLOOR = 1 // B/s

/**
 * Velocidade do painel de uploads: amostras em janelas de 1s suavizadas por EMA
 * (`0.75 * anterior + 0.25 * instantânea`), para o número dar tempo de ser lido.
 *
 * Duas coisas que a média sozinha faz errado e que aqui são tratadas:
 *
 *   - O total enviado pode **diminuir** (cancelar um item, limpar os concluídos, um
 *     pedaço que falhou e volta do zero). A diferença negativa não é velocidade
 *     nenhuma: a janela vira apenas uma reancoragem, senão o painel marcaria 0 até o
 *     envio reconquistar os bytes perdidos — que num arquivo de gigabytes leva minutos.
 *   - Uma EMA só encolhe: sem byte nenhum ela nunca chega a zero, e uma parada longa
 *     deixava ligada uma velocidade infinitesimal, que imprimia "0,00 B/s" e, dividindo
 *     o que falta por ela, uma previsão de término absurda (`2.16e+68h`). Abaixo do piso
 *     a velocidade é zero, e quem mostra decide o que dizer de uma fila parada.
 */
export class SpeedMeter {
  private speed = 0
  private lastTick: number
  private lastSent = 0

  constructor(now: number) {
    this.lastTick = now
  }

  /** Registra o total já enviado e devolve a velocidade atual em bytes por segundo. */
  sample(bytesDone: number, now: number, active: boolean): number {
    const dt = (now - this.lastTick) / 1000
    if (dt >= 1) {
      const delta = bytesDone - this.lastSent
      this.lastTick = now
      this.lastSent = bytesDone
      if (delta >= 0) {
        const inst = delta / dt
        this.speed = this.speed === 0 ? inst : this.speed * 0.75 + inst * 0.25
      }
      if (!active || this.speed < FLOOR) this.speed = 0
    }
    return this.speed
  }

  get value(): number {
    return this.speed
  }
}
