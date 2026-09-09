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
