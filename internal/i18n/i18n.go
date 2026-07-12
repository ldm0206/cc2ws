// Package i18n is a zero-dependency bilingual (zh/en) string table.
// Default language is zh. Both the GUI and TUI import it.
package i18n

import "sync"

type Lang string

const (
	ZH Lang = "zh"
	EN Lang = "en"

	Default Lang = ZH
)

var (
	mu      sync.RWMutex
	current Lang = ZH
)

// SetLang sets the active language. Unknown values fall back to zh at lookup.
func SetLang(l Lang) {
	mu.Lock()
	current = l
	mu.Unlock()
}

// Current returns the active language.
func Current() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T returns the localized string for key. It tries the current language, then
// zh, then returns the key itself — never empty, never panics.
func T(key string) string {
	mu.RLock()
	lang := current
	mu.RUnlock()
	if lang != ZH && lang != EN {
		lang = ZH
	}
	if s, ok := tableFor(lang)[key]; ok {
		return s
	}
	if s, ok := zh[key]; ok {
		return s
	}
	return key
}

func tableFor(l Lang) map[string]string {
	if l == EN {
		return en
	}
	return zh
}
