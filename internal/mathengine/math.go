// Package mathengine generates verifiable problems using exact rational arithmetic.
package mathengine

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"sort"
	"strings"
)

type Problem struct {
	Kind       string    `json:"kind"`
	Difficulty string    `json:"difficulty"`
	Prompt     string    `json:"prompt"`
	Latex      string    `json:"latex"`
	Hints      []string  `json:"hints"`
	Steps      []string  `json:"steps,omitempty"`
	Expected   []string  `json:"expected,omitempty"`
	Revealed   bool      `json:"revealed"`
	Attempts   []Attempt `json:"attempts"`
}
type Attempt struct {
	Answer  string `json:"answer"`
	Correct bool   `json:"correct"`
}

var Kinds = []string{"arithmetic", "linear", "quadratic", "system", "percent", "fraction"}

func Generate(kind, difficulty string) (Problem, error) {
	if difficulty == "" {
		difficulty = "medium"
	}
	if kind == "" {
		kind = "quadratic"
	}
	scale := 10
	switch difficulty {
	case "easy":
		scale = 5
	case "medium":
	case "hard":
		scale = 30
	default:
		return Problem{}, fmt.Errorf("difficulty must be easy, medium or hard")
	}
	n := func() int { return rand.IntN(scale) + 1 }
	a, b := n(), n()
	p := Problem{Kind: kind, Difficulty: difficulty, Attempts: []Attempt{}}
	switch kind {
	case "arithmetic":
		c := n()
		p.Prompt = "Вычислите значение выражения."
		p.Latex = fmt.Sprintf("%d + %d \\times %d", a, b, c)
		p.Expected = []string{fmt.Sprint(a + b*c)}
		p.Hints = []string{"Сначала выполните умножение."}
		p.Steps = []string{fmt.Sprintf("%d × %d = %d", b, c, b*c), fmt.Sprintf("%d + %d = %d", a, b*c, a+b*c)}
	case "linear":
		x := n() - scale/2
		c := a*x + b
		p.Prompt = "Найдите x."
		p.Latex = fmt.Sprintf("%dx + %d = %d", a, b, c)
		p.Expected = []string{fmt.Sprint(x)}
		p.Hints = []string{"Перенесите постоянное слагаемое вправо, затем разделите на коэффициент при x."}
		p.Steps = []string{fmt.Sprintf("%dx = %d − %d = %d", a, c, b, c-b), fmt.Sprintf("x = %d/%d = %d", c-b, a, x)}
	case "quadratic":
		b = a + b
		p.Prompt = "Найдите все корни уравнения. Разделяйте ответы точкой с запятой."
		p.Latex = fmt.Sprintf("x^2 - %dx + %d = 0", a+b, a*b)
		p.Expected = []string{fmt.Sprint(a), fmt.Sprint(b)}
		p.Hints = []string{"Найдите два числа, сумма и произведение которых совпадают с коэффициентами по теореме Виета."}
		p.Steps = []string{fmt.Sprintf("%d + %d = %d; %d × %d = %d", a, b, a+b, a, b, a*b), fmt.Sprintf("(x − %d)(x − %d) = 0", a, b), fmt.Sprintf("x = %d; x = %d", a, b)}
	case "system":
		p.Prompt = "Найдите x и y. Ответ: x=…; y=… (или два числа в порядке x, y)."
		p.Latex = fmt.Sprintf("\\begin{cases}x+y=%d\\\\2x-y=%d\\end{cases}", a+b, 2*a-b)
		p.Expected = []string{fmt.Sprint(a), fmt.Sprint(b)}
		p.Hints = []string{"Сложите уравнения, чтобы исключить y."}
		p.Steps = []string{fmt.Sprintf("3x = %d + (%d) = %d", a+b, 2*a-b, 3*a), fmt.Sprintf("x = %d", a), fmt.Sprintf("y = %d − %d = %d", a+b, a, b)}
	case "percent":
		p.Prompt = fmt.Sprintf("Найдите %d%% от %d.", a, b*10)
		p.Latex = fmt.Sprintf("%d\\%% \\text{ от } %d", a, b*10)
		v := big.NewRat(int64(a*b), 10)
		p.Expected = []string{v.RatString()}
		p.Hints = []string{"Процент — это одна сотая часть числа."}
		p.Steps = []string{fmt.Sprintf("%d × %d / 100", b*10, a), "Ответ: " + v.RatString()}
	case "fraction":
		c, d := n(), n()
		v := new(big.Rat).Add(big.NewRat(int64(a), int64(b)), big.NewRat(int64(c), int64(d)))
		p.Prompt = "Сложите дроби. Ответ можно записать дробью или точной десятичной записью."
		p.Latex = fmt.Sprintf("\\frac{%d}{%d} + \\frac{%d}{%d}", a, b, c, d)
		p.Expected = []string{v.RatString()}
		p.Hints = []string{"Приведите дроби к общему знаменателю."}
		p.Steps = []string{fmt.Sprintf("(%d × %d + %d × %d) / (%d × %d)", a, d, c, b, b, d), fmt.Sprintf("%d/%d = %s", a*d+c*b, b*d, v.RatString())}
	default:
		return Problem{}, fmt.Errorf("unsupported math kind: %s", kind)
	}
	return p, nil
}
func rational(s string) (*big.Rat, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "−", "-"))
	s = strings.ReplaceAll(s, ",", ".")
	if len(s) == 0 || len(s) > 128 || strings.ContainsAny(s, "eE") {
		return nil, fmt.Errorf("use a number, decimal or fraction")
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("invalid number %q", s)
	}
	return r, nil
}

// Check accepts reordered root sets, exact decimals, reduced/unreduced fractions and named system variables.
func Check(p Problem, answer string) (bool, error) {
	if len(answer) > 512 {
		return false, fmt.Errorf("answer too long")
	}
	answer = strings.Trim(strings.TrimSpace(answer), "{}[]()")
	sep := ";"
	if !strings.Contains(answer, ";") && strings.Contains(answer, ",") && len(p.Expected) > 1 {
		sep = ","
	}
	parts := strings.Split(answer, sep)
	if len(parts) != len(p.Expected) {
		return false, nil
	}
	actual := make([]string, len(parts))
	named := false
	for _, v := range parts {
		if strings.Contains(v, "=") {
			named = true
		}
	}
	if p.Kind == "system" && named {
		labels := map[string]string{}
		for _, v := range parts {
			kv := strings.Split(v, "=")
			if len(kv) != 2 {
				return false, fmt.Errorf("use x=number; y=number")
			}
			k := strings.TrimSpace(kv[0])
			if k != "x" && k != "y" {
				return false, fmt.Errorf("unknown variable")
			}
			if _, exists := labels[k]; exists {
				return false, fmt.Errorf("duplicate variable")
			}
			labels[k] = kv[1]
		}
		if len(labels) != 2 {
			return false, nil
		}
		parts = []string{labels["x"], labels["y"]}
	}
	for i, v := range parts {
		if strings.Contains(v, "=") {
			kv := strings.Split(v, "=")
			label := strings.TrimSpace(kv[0])
			valid := label == "x" || p.Kind == "quadratic" && (label == "x1" || label == "x2")
			if len(kv) != 2 || !valid {
				return false, fmt.Errorf("invalid variable")
			}
			v = kv[1]
		}
		r, err := rational(v)
		if err != nil {
			return false, err
		}
		actual[i] = r.RatString()
	}
	expected := append([]string(nil), p.Expected...)
	for i, v := range expected {
		r, e := rational(v)
		if e != nil {
			return false, e
		}
		expected[i] = r.RatString()
	}
	if p.Kind == "quadratic" {
		sort.Strings(expected)
		sort.Strings(actual)
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return false, nil
		}
	}
	return true, nil
}
