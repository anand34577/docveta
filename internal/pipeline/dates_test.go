package pipeline

import (
	"testing"
	"time"
)

func TestExtractDate(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		text     string
		dayFirst bool
		want     string
	}{
		{"Bill Date: 05/08/2026 Due 20/08/2026", true, "2026-08-05"},
		{"Bill Date: 08/05/2026", false, "2026-08-05"},
		{"Invoice 2026-03-15", true, "2026-03-15"},
		{"Dated 5th August 2026", true, "2026-08-05"},
		{"August 5, 2026", true, "2026-08-05"},
		{"Issued 05-Aug-26", true, "2026-08-05"},
		{"Date 13/02/2026", false, "2026-02-13"}, // impossible MM/DD falls back to DD/MM
		{"No date here 12345", true, ""},
		{"Ref 99/99/2026 then 01.02.2025", true, "2025-02-01"},
		{"Future 01/01/2099", true, ""},
	}
	for _, c := range cases {
		got, ok := ExtractDate(c.text, c.dayFirst, now)
		s := ""
		if ok {
			s = got.Format("2006-01-02")
		}
		if s != c.want {
			t.Errorf("%q: got %q want %q", c.text, s, c.want)
		}
	}
}

func TestHasUsableText(t *testing.T) {
	if HasUsableText("   \n ") || HasUsableText("abc") {
		t.Fatal("short text should not be usable")
	}
	if !HasUsableText("This is a perfectly normal electricity bill text") {
		t.Fatal("normal text should be usable")
	}
	if !HasUsableText("यह एक सामान्य बिजली बिल का पाठ है जो पढ़ने योग्य है") {
		t.Fatal("Hindi text should be usable")
	}
	if HasUsableText("���������������������") {
		t.Fatal("garbage should not be usable")
	}
}

func TestLegacyIndicText(t *testing.T) {
	// A bilingual exam booklet set in a Walkman-Chanakya font: English is real text, the Hindi
	// lines are font codes. The page must be recognised, not trusted.
	chanakya := `MAIN TEST BOOKLET/◊ÈÅÿ ¬⁄UËˇÊÊ ¬ÈÁSÃ∑§Ê
This booklet contains 48 Printed pages.
ß‚ ¬ÈÁSÃ∑§Ê ◊¥ 48 ◊ÈÁŒ˝Ã ¬Îc∆U „Ò¥–
Do not open this Test Booklet until you are asked to do so.
ß‚ ¬⁄UËˇÊÊ ¬ÈÁSÃ∑§Ê ∑§Ê Ã’ Ã∑§ Ÿ πÊ‹¥ ¡’ Ã∑§ ∑§„Ê Ÿ ¡Ê∞–
Read carefully the Instructions on the Back Cover of this Test Booklet.
1. The OMR Answer Sheet is inside this Test Booklet. When you are directed to open it.`
	if !LegacyIndicText(chanakya) || HasUsableText(chanakya) {
		t.Error("Chanakya-encoded Hindi was taken as readable text")
	}
	kruti := ";g ,d ijh{kk iqfLrdk gS vkSj blesa 48 i`B gSaA vki bls rc rd u [kksysa tc rd dgk u tk,A ijh{kkFkhZ dk uke vkSj gLrk{kj Hkh nsa ds fy, dks dh esa ls"
	if !LegacyIndicText(kruti) {
		t.Error("Kruti Dev-encoded Hindi was taken as readable text")
	}
	for _, ok := range []string{
		"This is a perfectly normal electricity bill. Amount due ₹1,842.00 – due 05/08/2026 § 4 “Terms” apply; the candidate’s copy.",
		"Le contrôle de sécurité a été effectué à Zürich. Straße, garçon, déjà vu, ¿Qué tal? ﬁnal ﬂow — fine.",
		"यह एक सामान्य बिजली बिल का पाठ है जो पढ़ने योग्य है और इसमें कोई गड़बड़ी नहीं है।",
		"Invoice INV-2026-0042 for Tata Power, consumer number 1234-5678, billing period July 2026.",
	} {
		if LegacyIndicText(ok) {
			t.Errorf("normal text flagged as legacy font: %q", ok)
		}
	}
}
