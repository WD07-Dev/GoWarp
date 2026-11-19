package vm

import (
	"context"
	"strings"
	"testing"
)

func TestProcedureIgnoresSurplusDefaultArguments(t *testing.T) {
	for _, supplied := range []bool{true, false} {
		blocks := strings.Replace(procedureProject, `"argumentdefaults":"[2]"`, `"argumentdefaults":"[2,99,100]"`, 1)
		if !supplied {
			blocks = strings.Replace(blocks, `"callN":[1,[4,"3"]]`, "", 1)
		}
		r := fixture(t, blocks)
		run(t, r, 10, false)
		want := float64(104)
		if supplied {
			want = 109
		}
		if got := number(r.Snapshot()[0].Variables["score"].Value); got != want {
			t.Fatalf("supplied=%v got=%v want=%v", supplied, got, want)
		}
	}
}

func TestProcedureMissingOrMalformedDefaultsStillFail(t *testing.T) {
	for _, defaults := range []string{"[]", "[2,{}]"} {
		blocks := strings.Replace(procedureProject, `"argumentdefaults":"[2]"`, `"argumentdefaults":"`+defaults+`"`, 1)
		r := fixture(t, blocks)
		if err := r.Run(context.Background(), 10, 30, false); err == nil {
			t.Fatalf("accepted invalid defaults %s", defaults)
		}
	}
}
