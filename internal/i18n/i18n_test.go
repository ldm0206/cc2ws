package i18n

import "testing"

func TestDefaultsToZH(t *testing.T) {
	SetLang(Default)
	if Default != ZH {
		t.Fatalf("Default = %q, want zh", Default)
	}
	if got := T("upstream_base"); got == "" || got == "upstream_base" {
		t.Fatalf("T(upstream_base) = %q; zh table not wired", got)
	}
}

func TestSetLangSwitches(t *testing.T) {
	SetLang(EN)
	if got := T("upstream_base"); got == "" || got == "upstream_base" {
		t.Fatalf("EN T(upstream_base) = %q", got)
	}
	SetLang(ZH)
	if got := T("upstream_base"); got == "" || got == "upstream_base" {
		t.Fatalf("ZH T(upstream_base) = %q", got)
	}
}

func TestUnknownKeyFallsBack(t *testing.T) {
	SetLang(EN)
	if got := T("no_such_key_xyz"); got != "no_such_key_xyz" {
		t.Fatalf("unknown key = %q, want the key itself", got)
	}
}

func TestUnknownLangFallsBackToZH(t *testing.T) {
	current = "fr"
	if got := T("upstream_base"); got == "upstream_base" {
		t.Fatalf("unknown lang should fall back to zh table")
	}
	SetLang(Default)
}
