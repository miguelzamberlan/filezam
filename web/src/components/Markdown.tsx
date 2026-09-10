import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { resolveRelative } from '../lib/paths'

export interface MarkdownProps {
  text: string
  /** URL inline de um caminho relativo à pasta do arquivo; ausente quando irmãos não são alcançáveis (link público de arquivo). */
  assetUrl?: (relPath: string) => string
}

const EXTERNAL = /^(https?:|mailto:)/i

// Renderiza para elementos React (sem dangerouslySetInnerHTML). HTML bruto do arquivo é descartado (skipHtml)
// e o defaultUrlTransform do react-markdown já zera javascript:/data: etc. antes de chegar aqui.
// Imagens externas continuam bloqueadas pela CSP da SPA (img-src 'self').
export default function Markdown({ text, assetUrl }: MarkdownProps) {
  const local = (href: string | undefined) => {
    if (!href || !assetUrl || EXTERNAL.test(href)) return null
    const rel = resolveRelative('', href)
    return rel === null ? null : assetUrl(rel)
  }
  const components: Components = {
    a: ({ href, children }) => {
      if (href && EXTERNAL.test(href)) return <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>
      const url = local(href)
      // relativo: abre o arquivo como texto/inline em outra aba; âncoras e o resto viram texto simples
      return url ? <a href={url} target="_blank" rel="noopener noreferrer">{children}</a> : <span>{children}</span>
    },
    img: ({ src, alt }) => {
      const s = typeof src === 'string' ? src : undefined
      const url = s && EXTERNAL.test(s) ? s : local(s)
      return url ? <img src={url} alt={alt ?? ''} loading="lazy" /> : <span>{alt}</span>
    },
    table: ({ children }) => (
      <div className="overflow-x-auto">
        <table>{children}</table>
      </div>
    ),
  }
  return (
    <div className="markdown">
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={components}>
        {text}
      </ReactMarkdown>
    </div>
  )
}
