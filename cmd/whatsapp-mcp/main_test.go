package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestDefaultHTTPAddr(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"--http"}, []string{"--http", defaultHTTP}},
		{[]string{"--http", "--data", "x"}, []string{"--http", defaultHTTP, "--data", "x"}},
		{[]string{"--http", ":9000"}, []string{"--http", ":9000"}},
		{[]string{"--data", "x"}, []string{"--data", "x"}},
	}
	for _, c := range cases {
		if got := defaultHTTPAddr(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseInterleaved(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	delay := fs.Int("delay", 60, "")
	groups := fs.Bool("groups", false, "")
	data := fs.String("data", "", "")
	pos := parseInterleaved(fs, []string{"Busy now", "--delay", "45", "soon", "--groups", "--data", "D"})
	if !reflect.DeepEqual(pos, []string{"Busy now", "soon"}) || *delay != 45 || !*groups || *data != "D" {
		t.Fatalf("pos=%v delay=%d groups=%v data=%q", pos, *delay, *groups, *data)
	}
}
