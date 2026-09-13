// Filezam - https://github.com/miguelzamberlan/filezam
// Copyright (C) 2026 Miguel Zamberlan
// SPDX-License-Identifier: AGPL-3.0-only
//
// Distribuído sob a GNU Affero General Public License v3 (LICENSE), sem garantia.
// Aviso legal protegido pela seção 7(b) da AGPLv3 e pelo NOTICE.md: remover ou
// alterar este cabeçalho viola a licença e os direitos autorais do autor.

import { ApiError } from '../api/client'

export interface XhrHandle {
  promise: Promise<unknown>
  abort: () => void
}

/** Send a body with upload progress via XMLHttpRequest. Resolves with parsed JSON. */
export function xhrSend(
  method: string,
  url: string,
  body: Blob | FormData | null,
  onProgress?: (loaded: number) => void,
  headers: Record<string, string> = {},
): XhrHandle {
  const xhr = new XMLHttpRequest()
  const promise = new Promise<unknown>((resolve, reject) => {
    xhr.open(method, url, true)
    xhr.setRequestHeader('X-Filezam', '1')
    for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v)
    xhr.responseType = 'json'
    if (onProgress) xhr.upload.onprogress = (ev) => onProgress(ev.loaded)
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr.response)
      } else {
        const err = xhr.response?.error
        const { code, message, ...extra } = err ?? {}
        reject(new ApiError(xhr.status, code ?? 'internal', message ?? xhr.statusText, extra))
      }
    }
    xhr.onerror = () => reject(new ApiError(0, 'network', 'network error'))
    xhr.onabort = () => reject(new ApiError(0, 'aborted', 'aborted'))
    xhr.ontimeout = () => reject(new ApiError(0, 'network', 'timeout'))
    xhr.send(body)
  })
  return { promise, abort: () => xhr.abort() }
}
