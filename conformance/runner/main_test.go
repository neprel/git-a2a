package main

import (
	"os"
	"reflect"
	"testing"
)

func TestMergeEnvironmentAppendsInheritedPath(t *testing.T) {
	got := mergeEnvironment([]string{"PATH=/system", "A=old"}, map[string]string{"PATH": "/case" + string(os.PathListSeparator), "A": "new"})
	want := []string{"A=new", "PATH=/case" + string(os.PathListSeparator) + "/system"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}
