package services

import "testing"

func FuzzParseSelector(f *testing.F) {
	// Seed with known-good inputs
	f.Add("env=production")
	f.Add("tier!=free")
	f.Add("region in (us-east,eu-west)")
	f.Add("status notin (archived,deleted)")
	f.Add("reviewed")
	f.Add("!deprecated")
	f.Add("env=production,tier!=free,region in (us-east,eu-west),reviewed")
	f.Add("")
	f.Add("a=1,b=2,c=3,d=4,e=5")
	f.Add("key in (a,b,c,d,e,f,g,h,i,j)")

	f.Fuzz(func(t *testing.T, input string) {
		reqs, err := ParseSelector(input)
		if err != nil {
			return // Parse errors expected for random input
		}
		// If parsing succeeds, BuildSelectorSQL must not panic
		clauses, args := BuildSelectorSQL(reqs)
		if len(clauses) != len(reqs) {
			t.Errorf("clauses count %d != reqs count %d for input %q", len(clauses), len(reqs), input)
		}
		_ = args
	})
}
