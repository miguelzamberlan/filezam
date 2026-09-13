package vfs

import "strings"

// cp850High mapeia os bytes 0x80–0xFF da code page 850 para as runas correspondentes. O zip só
// promete UTF-8 quando marca o bit 0x800 do descritor geral; sem ele, o nome vem na code page OEM
// do sistema que compactou. No Windows em português (e nos idiomas da Europa Ocidental) essa code
// page é a 850, e é nela que gravam o "Enviar para > Pasta compactada" e o 7-Zip.
//
// A CP437, a OEM do Windows em inglês, só coincide com a 850 em parte dos acentos: ç, á, é, í, ó, ú
// e ê caem no mesmo byte, mas ã, õ e as maiúsculas acentuadas não existem na 437 — lida como 437,
// "São Paulo" virava "S╞o Paulo" e "Configurações" virava "ConfiguraçΣes". Um zip de um Windows em
// inglês com nomes só em ASCII é igual nas duas.
var cp850High = []rune{
	'Ç', 'ü', 'é', 'â', 'ä', 'à', 'å', 'ç', 'ê', 'ë', 'è', 'ï', 'î', 'ì', 'Ä', 'Å',
	'É', 'æ', 'Æ', 'ô', 'ö', 'ò', 'û', 'ù', 'ÿ', 'Ö', 'Ü', 'ø', '£', 'Ø', '×', 'ƒ',
	'á', 'í', 'ó', 'ú', 'ñ', 'Ñ', 'ª', 'º', '¿', '®', '¬', '½', '¼', '¡', '«', '»',
	'░', '▒', '▓', '│', '┤', 'Á', 'Â', 'À', '©', '╣', '║', '╗', '╝', '¢', '¥', '┐',
	'└', '┴', '┬', '├', '─', '┼', 'ã', 'Ã', '╚', '╔', '╩', '╦', '╠', '═', '╬', '¤',
	'ð', 'Ð', 'Ê', 'Ë', 'È', 'ı', 'Í', 'Î', 'Ï', '┘', '┌', '█', '▄', '¦', 'Ì', '▀',
	'Ó', 'ß', 'Ô', 'Ò', 'õ', 'Õ', 'µ', 'þ', 'Þ', 'Ú', 'Û', 'Ù', 'ý', 'Ý', '¯', '´',
	'­', '±', '‗', '¾', '¶', '§', '÷', '¸', '°', '¨', '·', '¹', '³', '²', '■', ' ',
}

// cp850 converts a CP850-encoded name to UTF-8.
func cp850(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x80 {
			b.WriteByte(c)
		} else {
			b.WriteRune(cp850High[c-0x80])
		}
	}
	return b.String()
}
