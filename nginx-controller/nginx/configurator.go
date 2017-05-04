package nginx

import (
	"fmt"
	"strings"
	"sync"

	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/extensions"
)

const emptyHost = ""

// Configurator transforms an Ingress resource into NGINX Configuration
type Configurator struct {
	nginx  *NginxController
	config *Config
	lock   sync.Mutex
	hostRegistry HostRegistry
}

// NewConfigurator creates a new Configurator
func NewConfigurator(nginx *NginxController, config *Config, hr HostRegistry) *Configurator {
	cnf := Configurator{
		nginx:  nginx,
		config: config,
		hostRegistry : hr,
	}

	return &cnf
}

func (cnf *Configurator) AddOrUpdateDHParam(content string) (string, error) {
	return cnf.nginx.AddOrUpdateDHParam(content)
}

// AddOrUpdateIngress adds or updates NGINX configuration for an Ingress resource
func (cnf *Configurator) AddOrUpdateIngress(name string, ingEx *IngressEx) {
	cnf.lock.Lock()
	defer cnf.lock.Unlock()

	pems := cnf.updateCertificates(ingEx)
	nginxCfg := cnf.generateNginxCfg(ingEx, pems)
	cnf.nginx.AddOrUpdateIngress(name, nginxCfg)
	if err := cnf.nginx.Reload(); err != nil {
		glog.Errorf("Error when adding or updating ingress %q: %q", name, err)
	}
}

func (cnf *Configurator) updateCertificates(ingEx *IngressEx) map[string]string {
	pems := make(map[string]string)

	for _, tls := range ingEx.Ingress.Spec.TLS {
		secretName := tls.SecretName
		secret, exist := ingEx.Secrets[secretName]
		if !exist {
			continue
		}
		cert, ok := secret.Data[api.TLSCertKey]
		if !ok {
			glog.Warningf("Secret %v has no private key", secretName)
			continue
		}
		key, ok := secret.Data[api.TLSPrivateKeyKey]
		if !ok {
			glog.Warningf("Secret %v has no cert", secretName)
			continue
		}

		name := ingEx.Ingress.Namespace + "-" + secretName
		pemFileName := cnf.nginx.AddOrUpdateCertAndKey(name, string(cert), string(key))

		for _, host := range tls.Hosts {
			pems[host] = pemFileName
		}
		if len(tls.Hosts) == 0 {
			pems[emptyHost] = pemFileName
		}
	}

	return pems
}

func (cnf *Configurator) getRandomName(serverName string) (randomName string, err error) {
	if cnf.hostRegistry == nil {
		return RandomNameGenerator.GenerateName(serverName), nil
	}
	rn, err, exists := cnf.hostRegistry.GetRandomNameIfRegistered(serverName)
	if err != nil {
		return "", err
	}

	if !exists {
		rn = RandomNameGenerator.GenerateName(serverName)
		err = cnf.hostRegistry.Register(rn, serverName)
		if err != nil {
			return "", err
		}
	}

	return rn, nil
}

func (cnf *Configurator) generateNginxCfg(ingEx *IngressEx, pems map[string]string) IngressNginxConfig {
	ingCfg := cnf.createConfig(ingEx)

	upstreams := make(map[string]Upstream)

	wsServices := getWebsocketServices(ingEx)
	rewrites := getRewrites(ingEx)
	sslServices := getSSLServices(ingEx)

	if ingEx.Ingress.Spec.Backend != nil {
		name := getNameForUpstream(ingEx.Ingress, emptyHost, ingEx.Ingress.Spec.Backend.ServiceName)
		upstream := cnf.createUpstream(ingEx, name, ingEx.Ingress.Spec.Backend, ingEx.Ingress.Namespace)
		upstreams[name] = upstream
	}

	var servers []Server

	for _, rule := range ingEx.Ingress.Spec.Rules {
		if rule.IngressRuleValue.HTTP == nil {
			continue
		}

		serverName := rule.Host

		if rule.Host == emptyHost {
			glog.Warningf("Host field of ingress rule in %v/%v is empty", ingEx.Ingress.Namespace, ingEx.Ingress.Name)
		}

		server := Server{
			Name:                    serverName,
			HTTP2:                   ingCfg.HTTP2,
			ProxyProtocol:           ingCfg.ProxyProtocol,
			HSTS:                    ingCfg.HSTS,
			HSTSMaxAge:              ingCfg.HSTSMaxAge,
			HSTSIncludeSubdomains:   ingCfg.HSTSIncludeSubdomains,
			RealIPHeader:            ingCfg.RealIPHeader,
			SetRealIPFrom:           ingCfg.SetRealIPFrom,
			RealIPRecursive:         ingCfg.RealIPRecursive,
			ProxySslNoForceRedirect: ingCfg.ProxySslNoForceRedirect,
			ProxyHideHeaders:        ingCfg.ProxyHideHeaders,
			ProxyPassHeaders:        ingCfg.ProxyPassHeaders,
		}

		if pemFile, ok := pems[serverName]; ok {
			server.SSL = true
			server.SSLCertificate = pemFile
			server.SSLCertificateKey = pemFile
		}

		var locations []Location
		rootLocation := false

		for _, path := range rule.HTTP.Paths {
			upsName := getNameForUpstream(ingEx.Ingress, rule.Host, path.Backend.ServiceName)

			if _, exists := upstreams[upsName]; !exists {
				upstream := cnf.createUpstream(ingEx, upsName, &path.Backend, ingEx.Ingress.Namespace)
				upstreams[upsName] = upstream
			}

			loc := createLocation(pathOrDefault(path.Path), upstreams[upsName], &ingCfg, wsServices[path.Backend.ServiceName], rewrites[path.Backend.ServiceName], sslServices[path.Backend.ServiceName])
			locations = append(locations, loc)

			if loc.Path == "/" {
				rootLocation = true
			}
		}

		if rootLocation == false && ingEx.Ingress.Spec.Backend != nil {
			upsName := getNameForUpstream(ingEx.Ingress, emptyHost, ingEx.Ingress.Spec.Backend.ServiceName)
			loc := createLocation(pathOrDefault("/"), upstreams[upsName], &ingCfg, wsServices[ingEx.Ingress.Spec.Backend.ServiceName], rewrites[ingEx.Ingress.Spec.Backend.ServiceName], sslServices[ingEx.Ingress.Spec.Backend.ServiceName])
			locations = append(locations, loc)
		}

		server.Locations = locations
		servers = append(servers, server)


		if ingCfg.GenerateRandomHostname {
			randomNamedServer := server
			randomName, err := cnf.getRandomName(server.Name)
			if err == nil {
				randomNamedServer.Name = randomName
				servers = append(servers, randomNamedServer)
			}
		}
	}

	if len(ingEx.Ingress.Spec.Rules) == 0 && ingEx.Ingress.Spec.Backend != nil {
		server := Server{
			Name:                    emptyHost,
			HTTP2:                   ingCfg.HTTP2,
			ProxyProtocol:           ingCfg.ProxyProtocol,
			HSTS:                    ingCfg.HSTS,
			HSTSMaxAge:              ingCfg.HSTSMaxAge,
			HSTSIncludeSubdomains:   ingCfg.HSTSIncludeSubdomains,
			RealIPHeader:            ingCfg.RealIPHeader,
			SetRealIPFrom:           ingCfg.SetRealIPFrom,
			RealIPRecursive:         ingCfg.RealIPRecursive,
			ProxyHideHeaders:        ingCfg.ProxyHideHeaders,
			ProxyPassHeaders:        ingCfg.ProxyPassHeaders,
			ProxySslNoForceRedirect: ingCfg.ProxySslNoForceRedirect,
		}

		if pemFile, ok := pems[emptyHost]; ok {
			server.SSL = true
			server.SSLCertificate = pemFile
			server.SSLCertificateKey = pemFile
		}

		var locations []Location

		upsName := getNameForUpstream(ingEx.Ingress, emptyHost, ingEx.Ingress.Spec.Backend.ServiceName)

		loc := createLocation(pathOrDefault("/"), upstreams[upsName], &ingCfg, wsServices[ingEx.Ingress.Spec.Backend.ServiceName], rewrites[ingEx.Ingress.Spec.Backend.ServiceName], sslServices[ingEx.Ingress.Spec.Backend.ServiceName])
		locations = append(locations, loc)

		server.Locations = locations
		servers = append(servers, server)
	}

	return IngressNginxConfig{Upstreams: upstreamMapToSlice(upstreams), Servers: servers}
}

func (cnf *Configurator) createConfig(ingEx *IngressEx) Config {
	ingCfg := *cnf.config
	if proxyConnectTimeout, exists := ingEx.Ingress.Annotations["nginx.org/proxy-connect-timeout"]; exists {
		ingCfg.ProxyConnectTimeout = proxyConnectTimeout
	}
	if proxyReadTimeout, exists := ingEx.Ingress.Annotations["nginx.org/proxy-read-timeout"]; exists {
		ingCfg.ProxyReadTimeout = proxyReadTimeout
	}
	if proxyHideHeaders, exists, err := GetMapKeyAsStringSlice(ingEx.Ingress.Annotations, "nginx.org/proxy-hide-headers", ingEx.Ingress); exists {
		if err != nil {
			glog.Error(err)
		} else {
			ingCfg.ProxyHideHeaders = proxyHideHeaders
		}
	}
	if proxyPassHeaders, exists, err := GetMapKeyAsStringSlice(ingEx.Ingress.Annotations, "nginx.org/proxy-pass-headers", ingEx.Ingress); exists {
		if err != nil {
			glog.Error(err)
		} else {
			ingCfg.ProxyPassHeaders = proxyPassHeaders
		}
	}
	if clientMaxBodySize, exists := ingEx.Ingress.Annotations["nginx.org/client-max-body-size"]; exists {
		ingCfg.ClientMaxBodySize = clientMaxBodySize
	}
	if HTTP2, exists, err := GetMapKeyAsBool(ingEx.Ingress.Annotations, "nginx.org/http2", ingEx.Ingress); exists {
		if err != nil {
			glog.Error(err)
		} else {
			ingCfg.HTTP2 = HTTP2
		}
	}
	if proxyBuffering, exists, err := GetMapKeyAsBool(ingEx.Ingress.Annotations, "nginx.org/proxy-buffering", ingEx.Ingress); exists {
		if err != nil {
			glog.Error(err)
		} else {
			ingCfg.ProxyBuffering = proxyBuffering
		}
	}

	ingCfg.ProxySslNoForceRedirect = false
	if sslNoForceRedirect, exists, err := GetMapKeyAsBool(ingEx.Ingress.Annotations, "nginx.org/proxy-ssl-no-force-redirect", ingEx.Ingress); exists {
		if err != nil {
			glog.Error(err)
		} else {
			ingCfg.ProxySslNoForceRedirect = sslNoForceRedirect
		}
	}

	ingCfg.GenerateRandomHostname = false
	if doGenerateRandomHost, exists, err := GetMapKeyAsBool(ingEx.Ingress.Annotations, "nginx.org/generate-random-name", ingEx.Ingress); exists {
		if err != nil {
			glog.Error(err)
		} else {
			ingCfg.GenerateRandomHostname = doGenerateRandomHost
		}
	}

	if hsts, exists, err := GetMapKeyAsBool(ingEx.Ingress.Annotations, "nginx.org/hsts", ingEx.Ingress); exists {
		if err != nil {
			glog.Error(err)
		} else {
			parsingErrors := false

			hstsMaxAge, existsMA, err := GetMapKeyAsInt(ingEx.Ingress.Annotations, "nginx.org/hsts-max-age", ingEx.Ingress)
			if existsMA && err != nil {
				glog.Error(err)
				parsingErrors = true
			}
			hstsIncludeSubdomains, existsIS, err := GetMapKeyAsBool(ingEx.Ingress.Annotations, "nginx.org/hsts-include-subdomains", ingEx.Ingress)
			if existsIS && err != nil {
				glog.Error(err)
				parsingErrors = true
			}

			if parsingErrors {
				glog.Errorf("Ingress %s/%s: There are configuration issues with hsts annotations, skipping annotions for all hsts settings", ingEx.Ingress.GetNamespace(), ingEx.Ingress.GetName())
			} else {
				ingCfg.HSTS = hsts
				if existsMA {
					ingCfg.HSTSMaxAge = hstsMaxAge
				}
				if existsIS {
					ingCfg.HSTSIncludeSubdomains = hstsIncludeSubdomains
				}
			}
		}
	}

	if proxyBuffers, exists := ingEx.Ingress.Annotations["nginx.org/proxy-buffers"]; exists {
		ingCfg.ProxyBuffers = proxyBuffers
	}
	if proxyBufferSize, exists := ingEx.Ingress.Annotations["nginx.org/proxy-buffer-size"]; exists {
		ingCfg.ProxyBufferSize = proxyBufferSize
	}
	if proxyMaxTempFileSize, exists := ingEx.Ingress.Annotations["nginx.org/proxy-max-temp-file-size"]; exists {
		ingCfg.ProxyMaxTempFileSize = proxyMaxTempFileSize
	}
	return ingCfg
}

func getWebsocketServices(ingEx *IngressEx) map[string]bool {
	wsServices := make(map[string]bool)

	if services, exists := ingEx.Ingress.Annotations["nginx.org/websocket-services"]; exists {
		for _, svc := range strings.Split(services, ",") {
			wsServices[svc] = true
		}
	}

	return wsServices
}

func getRewrites(ingEx *IngressEx) map[string]string {
	rewrites := make(map[string]string)

	if services, exists := ingEx.Ingress.Annotations["nginx.org/rewrites"]; exists {
		for _, svc := range strings.Split(services, ";") {
			if serviceName, rewrite, err := parseRewrites(svc); err != nil {
				glog.Errorf("In %v nginx.org/rewrites contains invalid declaration: %v, ignoring", ingEx.Ingress.Name, err)
			} else {
				rewrites[serviceName] = rewrite
			}
		}
	}

	return rewrites
}

func parseRewrites(service string) (serviceName string, rewrite string, err error) {
	parts := strings.SplitN(service, " ", 2)

	if len(parts) != 2 {
		return "", "", fmt.Errorf("Invalid rewrite format: %s\n", service)
	}

	svcNameParts := strings.Split(parts[0], "=")
	if len(svcNameParts) != 2 {
		return "", "", fmt.Errorf("Invalid rewrite format: %s\n", svcNameParts)
	}

	rwPathParts := strings.Split(parts[1], "=")
	if len(rwPathParts) != 2 {
		return "", "", fmt.Errorf("Invalid rewrite format: %s\n", rwPathParts)
	}

	return svcNameParts[1], rwPathParts[1], nil
}

func getSSLServices(ingEx *IngressEx) map[string]bool {
	sslServices := make(map[string]bool)

	if services, exists := ingEx.Ingress.Annotations["nginx.org/ssl-services"]; exists {
		for _, svc := range strings.Split(services, ",") {
			sslServices[svc] = true
		}
	}

	return sslServices
}

func createLocation(path string, upstream Upstream, cfg *Config, websocket bool, rewrite string, ssl bool) Location {
	loc := Location{
		Path:                 path,
		Upstream:             upstream,
		ProxyConnectTimeout:  cfg.ProxyConnectTimeout,
		ProxyReadTimeout:     cfg.ProxyReadTimeout,
		ClientMaxBodySize:    cfg.ClientMaxBodySize,
		Websocket:            websocket,
		Rewrite:              rewrite,
		SSL:                  ssl,
		ProxyBuffering:       cfg.ProxyBuffering,
		ProxyBuffers:         cfg.ProxyBuffers,
		ProxyBufferSize:      cfg.ProxyBufferSize,
		ProxyMaxTempFileSize: cfg.ProxyMaxTempFileSize,
	}

	return loc
}

func (cnf *Configurator) createUpstream(ingEx *IngressEx, name string, backend *extensions.IngressBackend, namespace string) Upstream {
	ups := NewUpstreamWithDefaultServer(name)

	endps, exists := ingEx.Endpoints[backend.ServiceName+backend.ServicePort.String()]
	if exists {
		var upsServers []UpstreamServer
		for _, endp := range endps {
			addressport := strings.Split(endp, ":")
			upsServers = append(upsServers, UpstreamServer{addressport[0], addressport[1]})
		}
		if len(upsServers) > 0 {
			ups.UpstreamServers = upsServers
		}
	}

	return ups
}

func pathOrDefault(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

func getNameForUpstream(ing *extensions.Ingress, host string, service string) string {
	return fmt.Sprintf("%v-%v-%v-%v", ing.Namespace, ing.Name, host, service)
}

func upstreamMapToSlice(upstreams map[string]Upstream) []Upstream {
	result := make([]Upstream, 0, len(upstreams))

	for _, ups := range upstreams {
		result = append(result, ups)
	}

	return result
}

// DeleteIngress deletes NGINX configuration for an Ingress resource
func (cnf *Configurator) DeleteIngress(name string) {
	cnf.lock.Lock()
	defer cnf.lock.Unlock()

	cnf.nginx.DeleteIngress(name)
	if err := cnf.nginx.Reload(); err != nil {
		glog.Errorf("Error when removing ingress %q: %q", name, err)
	}
}

// UpdateEndpoints updates endpoints in NGINX configuration for an Ingress resource
func (cnf *Configurator) UpdateEndpoints(name string, ingEx *IngressEx) {
	cnf.AddOrUpdateIngress(name, ingEx)
}

// UpdateConfig updates NGINX Configuration parameters
func (cnf *Configurator) UpdateConfig(config *Config) {
	cnf.lock.Lock()
	defer cnf.lock.Unlock()

	cnf.config = config
	mainCfg := &NginxMainConfig{
		ServerNamesHashBucketSize: config.MainServerNamesHashBucketSize,
		ServerNamesHashMaxSize:    config.MainServerNamesHashMaxSize,
		LogFormat:                 config.MainLogFormat,
		SSLProtocols:              config.MainServerSSLProtocols,
		SSLCiphers:                config.MainServerSSLCiphers,
		SSLDHParam:                config.MainServerSSLDHParam,
		SSLPreferServerCiphers:    config.MainServerSSLPreferServerCiphers,
	}

	cnf.nginx.UpdateMainConfigFile(mainCfg)
}
