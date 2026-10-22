package tz

import "testing"

func TestLoad(t *testing.T) {
	l, w := Load("Europe/London")
	if w != "" || l.String() != "Europe/London" {
		t.Fatal(l, w)
	}
	l, w = Load("GMT Standard Time")
	if w != "" || l.String() != "Europe/London" {
		t.Fatal(l, w)
	}
	l, w = Load("Mars/Olympus")
	if w == "" || l.String() != "UTC" {
		t.Fatal(l, w)
	}
	if l, w = Load(""); w == "" || l.String() != "UTC" {
		t.Fatal("empty")
	}
}
