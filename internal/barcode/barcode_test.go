package barcode

import "testing"

func TestRoundTrip(t *testing.T) {
	sheet, err := SeparatorSheet()
	if err != nil {
		t.Fatal(err)
	}
	got := Decode(sheet)
	if len(got) != 1 || !IsSeparator(got[0]) {
		t.Fatalf("separator sheet decoded as %v", got)
	}
	label, err := ASNLabel(42)
	if err != nil {
		t.Fatal(err)
	}
	got = Decode(label)
	if len(got) != 1 {
		t.Fatalf("label decoded as %v", got)
	}
	if n, ok := ASN(got[0]); !ok || n != 42 {
		t.Fatalf("ASN %v %v from %q", n, ok, got[0])
	}
}

func TestParsing(t *testing.T) {
	for in, want := range map[string]int64{"ASN00042": 42, "asn 7": 7, "ASN123456": 123456} {
		if n, ok := ASN(in); !ok || n != want {
			t.Errorf("ASN(%q) = %d %v", in, n, ok)
		}
	}
	for _, in := range []string{"", "ASN", "ASN0", "INV-42", "42"} {
		if _, ok := ASN(in); ok {
			t.Errorf("ASN(%q) should not parse", in)
		}
	}
	for _, s := range []string{"PATCHT", "patcht", " DOCVETA-SEPARATOR "} {
		if !IsSeparator(s) {
			t.Errorf("%q should be a separator", s)
		}
	}
	if IsSeparator("PATCH") || IsSeparator("ASN00001") {
		t.Error("false positive separator")
	}
}
