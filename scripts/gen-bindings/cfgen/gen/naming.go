package gen

import (
	"strings"
	"unicode"
)

// acronyms maps a lowercased word to its Go-style acronym rendering, per
// tmp/06-codegen-spec.md section 1.3 "命名".
var acronyms = map[string]string{
	"id":    "ID",
	"url":   "URL",
	"uri":   "URI",
	"ttl":   "TTL",
	"http":  "HTTP",
	"https": "HTTPS",
	"json":  "JSON",
	"ip":    "IP",
	"tls":   "TLS",
	"md5":   "MD5",
	"sha":   "SHA",
	"api":   "API",
	"html":  "HTML",
	"css":   "CSS",
	"sql":   "SQL",
	"db":    "DB",
	"ai":    "AI",
	"cf":    "CF",
	"ja3":   "JA3",
	"ja4":   "JA4",
	"asn":   "ASN",
	"uuid":  "UUID",
}

// goKeywords are Go reserved words and predeclared identifiers that would
// collide with a generated identifier.
var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

// splitWords splits a camelCase (or PascalCase) identifier into its
// constituent words, keeping runs of uppercase letters (acronyms) together
// except for the last letter, which starts the following word (e.g.
// "HTTPServer" -> ["HTTP", "Server"]).
func splitWords(s string) []string {
	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return nil
	}
	var words []string
	start := 0
	for i := 1; i < n; i++ {
		prev, cur := runes[i-1], runes[i]
		boundary := false
		if (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(cur) {
			boundary = true
		} else if unicode.IsUpper(prev) && unicode.IsUpper(cur) && i+1 < n && unicode.IsLower(runes[i+1]) {
			boundary = true
		} else if prev == '_' || prev == '-' || prev == ' ' {
			boundary = true
		}
		if boundary {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	words = append(words, string(runes[start:]))
	// Drop separator-only fragments left over from '_'/'-'/' ' boundaries.
	out := words[:0]
	for _, w := range words {
		w = strings.Trim(w, "_- ")
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// pascalCase converts a camelCase or snake_case identifier to PascalCase,
// expanding known acronyms per the table above.
func pascalCase(s string) string {
	words := splitWords(s)
	var sb strings.Builder
	for _, w := range words {
		lw := strings.ToLower(w)
		if repl, ok := acronyms[lw]; ok {
			sb.WriteString(repl)
			continue
		}
		r := []rune(w)
		sb.WriteString(strings.ToUpper(string(r[0])))
		if len(r) > 1 {
			sb.WriteString(string(r[1:]))
		}
	}
	return sb.String()
}

// exportedName converts an IR member/decl name to its default exported Go
// name (PascalCase + acronym table + keyword escaping). rename overrides
// are applied by the caller before falling back to this.
func exportedName(s string) string {
	name := pascalCase(s)
	if goKeywords[name] {
		name += "_"
	}
	return name
}

// unexportedName lowercases the first rune of an exported name, used for
// unexported helper function names such as "rateLimitOutcomeFromJS".
func unexportedName(exported string) string {
	if exported == "" {
		return exported
	}
	r := []rune(exported)
	r[0] = unicode.ToLower(r[0])
	name := string(r)
	if goKeywords[name] {
		name += "_"
	}
	return name
}
