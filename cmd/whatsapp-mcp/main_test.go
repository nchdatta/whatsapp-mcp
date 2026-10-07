package main

import (
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
