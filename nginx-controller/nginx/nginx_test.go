package nginx


import "fmt"
import "testing"


import "k8s.io/kubernetes/pkg/api"
import "k8s.io/kubernetes/pkg/apis/extensions"
import "k8s.io/kubernetes/pkg/util/intstr"

type S struct {
	Name string
}

func ruleByIndex(i int) extensions.IngressRule {
    pathPrefix := "/"
    appPrefix := "foobar"
    httpContainerPort := 8080

    return extensions.IngressRule{
        Host: fmt.Sprintf("foo%d.bar.com", i),
        IngressRuleValue: extensions.IngressRuleValue{
            HTTP: &extensions.HTTPIngressRuleValue{
                Paths: []extensions.HTTPIngressPath{
                    {
                        Path: fmt.Sprintf("/%v%d", pathPrefix, i),
                        Backend: extensions.IngressBackend{
                            ServiceName: fmt.Sprintf("%v%d", appPrefix, i),
                            ServicePort: intstr.FromInt(httpContainerPort),
                        },
                    },
                },
            },
        },
    }
}

func createIngress() extensions.Ingress {
    appPrefix := "foobar"
    start := 1
    num := 2
    ns := "ns"
    httpContainerPort := 8080

    annotations := make(map[string]string)
    annotations["nginx.org/generate-random-name"] = "true"

    ing := extensions.Ingress{
        ObjectMeta: api.ObjectMeta{
            Name:      fmt.Sprintf("%v%d", appPrefix, start),
            Namespace: ns,
            Annotations: annotations,
        },
        Spec: extensions.IngressSpec{
            Backend: &extensions.IngressBackend{
                ServiceName: fmt.Sprintf("%v%d", appPrefix, start),
                ServicePort: intstr.FromInt(httpContainerPort),
            },
            Rules: []extensions.IngressRule{},
        },
    }
    for i := start; i < start+num; i++ {
        ing.Spec.Rules = append(ing.Spec.Rules, ruleByIndex(i))
    }
    return ing
}

func TestRandomGeneratedNames(t *testing.T) {
    nginxConfPath := "."
    local := false
    healthStatus := false
    nginxController, err := NewNginxController(nginxConfPath, local, healthStatus)
    if err != nil {
        t.FailNow()
    }
    config := NewDefaultConfig()
    config.GenerateRandomHostname = true
    configurator := NewConfigurator(nginxController, config)


    
    kIngress := createIngress()
    kSecrets := make(map[string]*api.Secret)
    kEndpoints := make(map[string][]string)


    ingEx := IngressEx{
        Ingress : &kIngress,
        Secrets : kSecrets,
        Endpoints : kEndpoints,
    }


    pems := make(map[string]string)

    ingressConfig := configurator.generateNginxCfg(&ingEx, pems)

    fmt.Print("Templating")
    nginxController.templateIt(ingressConfig, "nginx.conf")

    fmt.Print(ingressConfig)
}


func TestRandom(t *testing.T) {
	s1 := S{Name : "test1"}
	s2 := s1

	s2.Name = "test2"
	fmt.Print(s1)
	fmt.Print(s2)
	fmt.Print(RandStringBytesMaskImpr(10))
}