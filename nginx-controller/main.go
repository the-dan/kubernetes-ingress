package main

import (
	"flag"
	"time"

	"github.com/golang/glog"

	"github.com/nginxinc/kubernetes-ingress/nginx-controller/controller"
	"github.com/nginxinc/kubernetes-ingress/nginx-controller/nginx"
	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"

)

var (
	// Set during build
	version string

	healthStatus = flag.Bool("health-status", false,
		`If present, the default server listening on port 80 with the health check
		location "/nginx-health" gets added to the main nginx configuration.`)

	proxyURL = flag.String("proxy", "",
		`If specified, the controller assumes a kubctl proxy server is running on the
		given url and creates a proxy client. Regenerated NGINX configuration files
    are not written to the disk, instead they are printed to stdout. Also NGINX
    is not getting invoked. This flag is for testing.`)

	watchNamespace = flag.String("watch-namespace", api.NamespaceAll,
		`Namespace to watch for Ingress/Services/Endpoints. By default the controller
		watches acrosss all namespaces`)

	nginxConfigMaps = flag.String("nginx-configmaps", "",
		`Specifies a configmaps resource that can be used to customize NGINX
		configuration. The value must follow the following format: <namespace>/<name>`)

	etcdNodes = flag.String("etcd-nodes", "",
		`Specified comma-delimited list of etcd key-value API URLs`)

	outOfCluster = flag.Bool("out-of-cluster", false, 
		`If present, assume the controller lives outside of kubernetes cluster.
		Control over nginx are on {initv,systemd,upstart} shoulders.
		We just generate config and tell nginx to reload it.
		Kubernetes API must be accessible.
		Also pod IPs must be routeable from the controller`)

	nginxBinaryPath = flag.String("nginx-binary", "nginx",
		`Path to nginx binary to start, reload and test configs`)

	nginxConfPath = flag.String("nginx-conf", "/etc/nginx",
		`Path to nginx config directory`)
)

func main() {
	flag.Parse()

	glog.Infof("Starting NGINX Ingress controller Version %v\n", version)

	var kubeClient *client.Client
	var local = false

	if *proxyURL != "" {
		kubeClient = client.NewOrDie(&client.Config{
			Host: *proxyURL,
		})
		local = false
	} else {
		var err error
		kubeClient, err = client.NewInCluster()
		if err != nil {
			glog.Fatalf("Failed to create client: %v.", err)
		}
	}

	hostRegistry, err := nginx.NewHostRegistry(*etcdNodes)
	if err != nil {
		glog.Infof("Failed to create etcd client. Using transient random domain names %v", err)
	}


	ngxc, _ := nginx.NewNginxController(*nginxConfPath, local, *healthStatus, *outOfCluster, *nginxBinaryPath)
	ngxc.Start()
	config := nginx.NewDefaultConfig()
	cnf := nginx.NewConfigurator(ngxc, config, hostRegistry)
	lbc, _ := controller.NewLoadBalancerController(kubeClient, 30*time.Second, *watchNamespace, cnf, *nginxConfigMaps)
	lbc.Run()
}
