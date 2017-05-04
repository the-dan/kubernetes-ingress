package nginx


import (
	"log"
	"time"
    "strings"
    "errors"

	"golang.org/x/net/context"
    "github.com/coreos/etcd/client"
)

type HostRegistry interface {
    GetRandomNameIfRegistered(string) (string, error, bool)
    Register(string, string) (error)
}

type EtcdHostRegistry struct {
	etcdClient client.Client
}

func NewHostRegistry(endpoints string) (hr HostRegistry, err error) {
    if endpoints == "" {
        return nil, errors.New("No endpoints specified")
    }

	eps := strings.Split(endpoints, ",")

	cfg := client.Config{
        Endpoints:               eps,
        Transport:               client.DefaultTransport,
        // set timeout per request to fail fast when the target endpoint is unavailable
        HeaderTimeoutPerRequest: time.Second,
    }

    client, err := client.New(cfg)
     if err != nil {
        log.Fatal(err)
        return nil, err
    }
    
    hr = &EtcdHostRegistry{etcdClient : client}
    return hr, err
}

func (hr *EtcdHostRegistry) GetRandomNameIfRegistered(hostname string) (randomName string, err error, exists bool) {
	kapi := client.NewKeysAPI(hr.etcdClient)

	// TODO: escape hostname, it might contain "/"
	// of course it is invalid domain name, but we
	// just don't want any garbage (subdirs) in our etcd store
	resp, err := kapi.Get(context.Background(), "/hosts/" + hostname, nil)
	if err != nil {
        if client.IsKeyNotFound(err) {
            return "", nil, false
        }
		log.Fatal(err)
		return "", err, false
	} else {

        log.Printf("Get is done. Metadata is %q\n", resp)
        // print value
        log.Printf("%q key has %q value\n", resp.Node.Key, resp.Node.Value)

		return resp.Node.Value, nil, true
	}
}

func (hr *EtcdHostRegistry) Register(randomName, hostname string) (err error) {

    kapi := client.NewKeysAPI(hr.etcdClient)
    // TODO: normalize hostname
    log.Printf("Setting '%s' key with '%s' value\n", hostname, randomName)
    resp, err := kapi.Set(context.Background(), "/hosts/" + hostname, randomName, nil)
    if err != nil {
        log.Fatal(err)
        return err
    } else {
        // print common key info
        log.Printf("Set is done. Metadata is %q\n", resp)
        return nil
    }
}

