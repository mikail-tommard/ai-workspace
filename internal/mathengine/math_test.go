package mathengine

import (
	"fmt"
	"strings"
	"testing"
)

func TestGeneratedAnswers(t *testing.T) {
	for _, kind := range Kinds {
		for _, difficulty := range []string{"easy", "medium", "hard"} {
			t.Run(kind+"/"+difficulty, func(t *testing.T) {
				for i := 0; i < 200; i++ {
					p, e := Generate(kind, difficulty)
					if e != nil {
						t.Fatal(e)
					}
					if len(p.Steps) < 2 || len(p.Hints) == 0 || p.Latex == "" {
						t.Fatal("missing problem content")
					}
					answer := strings.Join(p.Expected, ";")
					ok, e := Check(p, answer)
					if e != nil || !ok {
						t.Fatalf("generated %s: %v %v", answer, ok, e)
					}
					wrong := "999999"
					if len(p.Expected) == 2 {
						wrong += ";999999"
					}
					ok, e = Check(p, wrong)
					if e != nil || ok {
						t.Fatal("incorrect answer accepted")
					}
				}
			})
		}
	}
}
func TestEquivalentAnswers(t *testing.T) {
	for _, tc := range []struct {
		kind     string
		expected []string
		answer   string
		correct  bool
	}{{"fraction", []string{"1/2"}, "2/4", true}, {"fraction", []string{"1/2"}, "0.5", true}, {"fraction", []string{"1/2"}, "0,5", true}, {"fraction", []string{"1/3"}, "0.333", false}, {"quadratic", []string{"2", "3"}, "x=3; x=2", true}, {"quadratic", []string{"2", "3"}, "3,2", true}, {"system", []string{"2", "3"}, "y=3; x=2", true}, {"system", []string{"2", "3"}, "3;2", false}, {"linear", []string{"-2"}, "x=−2", true}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			ok, e := Check(Problem{Kind: tc.kind, Expected: tc.expected}, tc.answer)
			if e != nil || ok != tc.correct {
				t.Fatalf("%v %v", ok, e)
			}
		})
	}
}
func TestInvalid(t *testing.T) {
	if _, e := Generate("integral", "easy"); e == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, e := Generate("linear", "unknown"); e == nil {
		t.Fatal("unknown level accepted")
	}
	for _, a := range []string{"1/0", "NaN", "1e99", "<script>", strings.Repeat("1", 600)} {
		if _, e := Check(Problem{Expected: []string{"1"}}, a); e == nil {
			t.Fatalf("accepted %q", a)
		}
	}
}
