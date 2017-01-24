package nginx

import (
	"fmt"
	"testing"
)

func TestRegistry(t *testing.T) {
	hr, err := NewHostRegistry("http://127.0.0.1:2379")
	if err != nil {
		t.FailNow()
	}

	err = hr.Register("test-random", "test")
	if err != nil {
		t.FailNow()
	}

	rn, err, _ := hr.GetRandomNameIfRegistered("test")
	if err != nil {
		t.FailNow()
	}
	fmt.Println(rn)
}

