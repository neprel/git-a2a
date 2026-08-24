package template

import "testing"

func TestExpandRecognizesOnlyLowercaseWordPlaceholdersAndEscapesBraces(t *testing.T) {
	got, err := Expand(`{"module":"{module}","literal":"{{word}}","other":{},"upper":"{Word}"}`, map[string]string{"module": "acme-lib"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"module":"acme-lib","literal":"{word}","other":{},"upper":"{Word}"}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	t.Logf("literal brace expansion: %s", got)
	if _, err := Expand(`{"value":"{typo}"}`, map[string]string{"module": "acme-lib"}); err == nil || err.Error() != "unsupported placeholder {typo}" {
		t.Fatalf("error = %v", err)
	}
}

func TestNamesIgnoresLiteralAndEscapedBraces(t *testing.T) {
	got := Names(`{"module":"{module}","literal":"{{word}}","other":{},"again":"{module}"}`)
	if len(got) != 1 || got[0] != "module" {
		t.Fatalf("names = %v", got)
	}
}
