// Espelho de vfs.Fold (internal/vfs/fold.go): forma usada para comparar nomes sem diferenciar
// maiúsculas, acentos e cedilha ("Relatório de AÇÃO" → "relatorio de acao"). O servidor
// pesquisa com a versão em Go; esta serve ao filtro da pasta e à digitação rápida.
const special: Record<string, string> = { ß: 'ss', æ: 'ae', œ: 'oe', ø: 'o', đ: 'd', ð: 'd', ł: 'l', ı: 'i', þ: 'th', ħ: 'h' }

export function fold(s: string): string {
  return s
    .normalize('NFKD')
    .replace(/\p{Mn}/gu, '')
    .toLowerCase()
    .replace(/[ßæœøđðłıþħ]/g, (c) => special[c])
}
