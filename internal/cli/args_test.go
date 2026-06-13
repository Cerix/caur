package cli

import (
	"reflect"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantOp  Op
		wantTgt []string
	}{
		{"install esplicito", []string{"-S", "yay"}, OpInstall, []string{"yay"}},
		{"install multiplo", []string{"-S", "foo", "bar"}, OpInstall, []string{"foo", "bar"}},
		{"upgrade", []string{"-Syu"}, OpUpgrade, nil},
		{"upgrade con target", []string{"-Syu", "foo"}, OpUpgrade, []string{"foo"}},
		{"refresh soltanto", []string{"-Sy"}, OpPassthrough, nil},
		{"ricerca -Ss", []string{"-Ss", "term"}, OpPassthrough, nil},
		{"info -Si", []string{"-Si", "pkg"}, OpPassthrough, nil},
		{"query", []string{"-Qi", "pkg"}, OpPassthrough, nil},
		{"remove", []string{"-R", "pkg"}, OpPassthrough, nil},
		{"clean", []string{"-Sc"}, OpPassthrough, nil},
		{"parole nude -> ricerca", []string{"firefox"}, OpPassthrough, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.args)
			if got.Op != c.wantOp {
				t.Errorf("Op = %v, atteso %v", got.Op, c.wantOp)
			}
			if !reflect.DeepEqual(got.Targets, c.wantTgt) {
				t.Errorf("Targets = %v, atteso %v", got.Targets, c.wantTgt)
			}
		})
	}
}

func TestClassifyUninstall(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"-Uni", "firefox"}, []string{"-Rns", "firefox"}},
		{[]string{"-Uni", "foo", "bar"}, []string{"-Rns", "foo", "bar"}},
		{[]string{"-Uni", "--noconfirm", "foo"}, []string{"-Rns", "--noconfirm", "foo"}},
		{[]string{"--uninstall", "foo"}, []string{"-Rns", "foo"}},
	}
	for _, c := range cases {
		got := Classify(c.args)
		if got.Op != OpPassthrough {
			t.Errorf("%v: Op = %v, atteso OpPassthrough", c.args, got.Op)
		}
		if !reflect.DeepEqual(got.Args, c.want) {
			t.Errorf("%v: Args = %v, atteso %v", c.args, got.Args, c.want)
		}
	}
}

func TestClassifyBareWordsRewrittenToSearch(t *testing.T) {
	got := Classify([]string{"firefox"})
	want := []string{"-Ss", "firefox"}
	if !reflect.DeepEqual(got.Args, want) {
		t.Errorf("Args = %v, atteso %v", got.Args, want)
	}
}
