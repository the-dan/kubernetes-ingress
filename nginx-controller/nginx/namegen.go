package nginx

import (
	"fmt"
	"strings"

	utilrand "k8s.io/kubernetes/pkg/util/rand"
)



// simpleNameGenerator generates random names.
type randomNameGenerator struct{}

// SimpleNameGenerator is a generator that returns the name plus a random suffix of five alphanumerics
// when a name is requested. The string is guaranteed to not exceed the length of a standard Kubernetes
// name (63 characters)
var RandomNameGenerator randomNameGenerator = randomNameGenerator{}

const (
	randomLength           = 10
)

func (randomNameGenerator) GenerateName(base string) string {
	dotN := strings.Index(base, ".")
	if dotN > -1 {
		hostname := base[:dotN]
		domainname := base[dotN+1:]
		return fmt.Sprintf("%s-%s.%s", hostname, utilrand.String(randomLength), domainname)
	} else {
		return fmt.Sprintf("%s-%s", base, utilrand.String(randomLength))
	}
}