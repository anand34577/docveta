package taxonomy

import "testing"

func TestRules(t *testing.T) {
	mt := NewMatchText("BESCOM Electricity Bill\nAccount 1234-5678\nबिजली बिल — Due date 20/08/2026")
	cases := []struct {
		rule Rule
		want bool
	}{
		{Rule{"any", "water electricity", false}, true},
		{Rule{"any", "water gas", false}, false},
		{Rule{"all", "bescom bill", false}, true},
		{Rule{"all", "bescom water", false}, false},
		{Rule{"all", `bescom "due date"`, false}, true},
		{Rule{"exact", "electricity bill", false}, true},
		{Rule{"exact", "Electricity Bill", true}, true},
		{Rule{"exact", "electricity bill", true}, false},
		{Rule{"regex", `Account \d{4}-\d{4}`, false}, true},
		{Rule{"regex", `[`, false}, false}, // invalid regex never matches
		{Rule{"fuzzy", "BESC0M Electricty", false}, true},
		{Rule{"fuzzy", "Airtel Broadband", false}, false},
		{Rule{"any", "बिजली", false}, true},
		{Rule{"none", "bescom", false}, false},
		{Rule{"any", "", false}, false},
	}
	for _, c := range cases {
		if got := c.rule.Matches(mt); got != c.want {
			t.Errorf("%+v: got %v want %v", c.rule, got, c.want)
		}
	}
}
