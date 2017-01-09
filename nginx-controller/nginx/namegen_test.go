package nginx

import "fmt"
import "testing"

func TestFQDN(t *testing.T) {
	s1 := RandomNameGenerator.GenerateName("foo.bar.com")
	s2 := RandomNameGenerator.GenerateName("foo")

	fmt.Print(s1)
	fmt.Print(s2)
}