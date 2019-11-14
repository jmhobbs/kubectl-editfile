package main

import "testing"

func TestSplitPodAndPath(t *testing.T) {
	pod, path := splitPodAndPath("pod-name:/remote/path")
	if pod != "pod-name" {
		t.Errorf("bad pod name\nexpected: \"pod-name\"\n   actual: %q", pod)
	}

	if path != "/remote/path" {
		t.Errorf("bad path\nexpected: \"/remote/path\"\n   actual: %q", path)
	}
}
