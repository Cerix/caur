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
		{"explicit install", []string{"-S", "yay"}, OpInstall, []string{"yay"}},
		{"multiple install", []string{"-S", "foo", "bar"}, OpInstall, []string{"foo", "bar"}},
		{"upgrade", []string{"-Syu"}, OpUpgrade, nil},
		{"upgrade with target", []string{"-Syu", "foo"}, OpUpgrade, []string{"foo"}},
		{"refresh only", []string{"-Sy"}, OpPassthrough, nil},
		{"search -Ss", []string{"-Ss", "term"}, OpPassthrough, nil},
		{"info -Si", []string{"-Si", "pkg"}, OpPassthrough, nil},
		{"query", []string{"-Qi", "pkg"}, OpPassthrough, nil},
		{"remove", []string{"-R", "pkg"}, OpPassthrough, nil},
		{"clean", []string{"-Sc"}, OpPassthrough, nil},
		{"bare words -> search", []string{"firefox"}, OpPassthrough, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.args)
			if got.Op != c.wantOp {
				t.Errorf("Op = %v, want %v", got.Op, c.wantOp)
			}
			if !reflect.DeepEqual(got.Targets, c.wantTgt) {
				t.Errorf("Targets = %v, want %v", got.Targets, c.wantTgt)
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
			t.Errorf("%v: Op = %v, want OpPassthrough", c.args, got.Op)
		}
		if !reflect.DeepEqual(got.Args, c.want) {
			t.Errorf("%v: Args = %v, want %v", c.args, got.Args, c.want)
		}
	}
}

func TestClassifyBareWordsRewrittenToSearch(t *testing.T) {
	got := Classify([]string{"firefox"})
	want := []string{"-Ss", "firefox"}
	if !reflect.DeepEqual(got.Args, want) {
		t.Errorf("Args = %v, want %v", got.Args, want)
	}
}
