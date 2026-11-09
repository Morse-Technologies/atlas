package catalog

import "testing"

func TestCoreTypesFallInTheirBands(t *testing.T) {
	cases := map[string]string{"procedure": BandProcedure, "policy": BandProcedure, "form": BandProcedure,
		"standard": BandNorm, "manual": BandManual, "contract": BandManual, "unheard-of": BandManual}
	for coreType, want := range cases {
		if got := bandFor(coreType); got != want {
			t.Errorf("bandFor(%s) = %s, want %s", coreType, got, want)
		}
	}
}

func TestLabelFallsBackToTheKey(t *testing.T) {
	if got := labelOf([]byte(`{"es":"Norma"}`), "standard"); got != "standard" {
		t.Fatalf("no English label: got %q, want the key", got)
	}
	if got := labelOf([]byte(`{"en":"Standard"}`), "standard"); got != "Standard" {
		t.Fatalf("got %q", got)
	}
}
