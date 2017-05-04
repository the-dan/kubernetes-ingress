package nginx

import (
	"log"
	)


type MockHostRegistry struct {
	m map[string]string
}

func NewMockHostRegistry() (hr *MockHostRegistry, err error) {
    m := make(map[string]string)
    hr = &MockHostRegistry{m : m}
    return hr, nil
}

func (hr *MockHostRegistry) GetRandomNameIfRegistered(hostname string) (randomName string, err error, exists bool) {
	randomName = hr.m[hostname]
	if randomName == "" {
		return "", nil, false
	}
	return randomName, nil, true
}

func (hr *MockHostRegistry) Register(randomName, hostname string) (err error) {

    log.Printf("Setting '%s' key with '%s' value\n", hostname, randomName)
    
    hr.m[hostname] = randomName

    return nil
}