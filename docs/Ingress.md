# Configuring Ingress

Using `port-forward` is great for testing, but you will ultimately want to
make it easier to access your Splunk cluster outside of Kubernetes. A common 
approach is using
[Kubernetes Ingress Controllers](https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/).

There are many Ingress Controllers available, each having their own pros and 
cons. There are just as many ways to configure each of them, which depend
upon  your specific infrastructure and organizational policies. Splunk
Operator will automatically create and manage Kubernetes Services for all
the relevant components, and we expect these will provide for easy integration
with most (if not all) Ingress Controllers and configurations.

```
$ kubectl get services -o name
service/splunk-cluster-cluster-master-service
service/splunk-cluster-deployer-service
service/splunk-cluster-indexer-headless
service/splunk-cluster-indexer-service
service/splunk-cluster-license-master-service
service/splunk-cluster-search-head-headless
service/splunk-cluster-search-head-service
```

Below we provide some examples for configuring two of the most popular Ingress controllers: the
[NGINX Ingress Controller](https://www.nginx.com/products/nginx/kubernetes-ingress-controller)
and [Istio](https://istio.io/). We hope these will serve as a useful starting
point to configuring ingress in your particular environment.

Before deploying an example, you will need to replace “example.com” with
whatever domain name you would like to use, and “example” in the service
names with the name of your `SplunkEnterprise` object. You will also need
to point your DNS for all the desired hostnames to the IP addresses of 
your ingress load balancer.


## Example: Configuring Ingress Using NGINX

For instructions on how to install and configure the NGINX Ingress Controller
for your specific infrastructure, please see its
[GitHub repository](https://github.com/nginxinc/kubernetes-ingress/).

It is highly recommended that you always use TLS encryption for your Splunk
endpoints. To do this, you will need to have one or more Kubernetes TLS
Secrets for all the hostnames you want to use with Splunk deployments.

If you are using [cert-manager](https://docs.cert-manager.io/en/latest/getting-started/)
with [Let’s Encrypt](https://letsencrypt.org/) to manage your TLS
certificates in Kubernetes, the following example Ingress object can be
used to enable secure (TLS) access to all Splunk components from outside of
your Kubernetes cluster:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: splunk-ingress
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/affinity: cookie
    certmanager.k8s.io/cluster-issuer: "letsencrypt-prod"
spec:
  rules:
  - host: splunk.example.com
    http:
      paths:
      - path: /
        backend:
          serviceName: splunk-example-search-head-service
          servicePort: 8000
      - path: /services/collector
        backend:
          serviceName: splunk-example-indexer-service
          servicePort: 8088
  - host: deployer.splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-example-deployer-service
          servicePort: 8000
  - host: cluster-master.splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-example-cluster-master-service
          servicePort: 8000
  - host: license-master.splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-example-license-master-service
          servicePort: 8000
  - host: spark-master.splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-example-spark-master-service
          servicePort: 8009
  tls:
  - hosts:
    - splunk.example.com
    - deployer.splunk.example.com
    - cluster-master.splunk.example.com
    - license-master.splunk.example.com
    - spark-master.splunk.example.com
    secretName: splunk.example.com-tls
```

The `certmanager.k8s.io/cluster-issuer` annotation can be optionally included
to automatically create and manage certificates for you. You may
need to change this to match your desired Issuer.

If you are not using cert-manager, you should remove this annotation and
update the `tls` section appropriately. If you are manually importing your
certificates into separate secrets for each hostname, you can reference these
by using multiple `tls` objects in your Ingress:

```yaml
tls:
  - hosts:
    - splunk.example.com
    secretName: splunk.example.com-tls
  - hosts:
    - deployer.splunk.example.com
    secretName: deployer.splunk.example.com-tls
  - hosts:
    - cluster-master.splunk.example.com
    secretName: cluster-master.splunk.example.com-tls
…
```


## Example: Configuring Ingress Using Istio

For instructions on how to install and configure Istio for your specific
infrastructure, please see its
[getting started guide](https://istio.io/docs/setup/getting-started/).

It is highly recommended that you always use TLS encryption for your Splunk
endpoints. To do this, you will need to have one or more Kubernetes TLS
Secrets for all the hostnames you want to use with Splunk deployments. Note
that these secrets must reside in the same namespace as your Istio Ingress
pod, most likely `istio-system`.

If you are using [cert-manager](https://docs.cert-manager.io/en/latest/getting-started/)
with [Let’s Encrypt](https://letsencrypt.org/) to manage your TLS certificates
in Kubernetes, the following example Certificate object can be created to 
populate a `splunk-example-com-tls` secret in the `istio-system` namespace:

```yaml
apiVersion: certmanager.k8s.io/v1alpha1
kind: Certificate
metadata:
  name: splunk-example-com-cert
  namespace: istio-system
spec:
  secretName: splunk-example-com-tls
  commonName: splunk.example.com
  dnsNames:
    - splunk.example.com
    - deployer.splunk.example.com
    - cluster-master.splunk.example.com
    - license-master.splunk.example.com
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
```

Next, you will need to create an Istio Gateway that is associated with your
certificates:

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: splunk-gw
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "splunk.example.com"
    - "deployer.splunk.example.com"
    - "cluster-master.splunk.example.com"
    - "license-master.splunk.example.com"
    tls:
      httpsRedirect: true
  - port:
      number: 443
      name: https
      protocol: HTTPS
    tls:
      mode: SIMPLE
      credentialName: "splunk-example-com-tls"
    hosts:
    - "splunk.example.com"
    - "deployer.splunk.example.com"
    - "cluster-master.splunk.example.com"
    - "license-master.splunk.example.com"
  - port:
      number: 9997
      name: tcp
      protocol: TCP
    hosts:
    - "*"
```

Note that `credentialName` references the same `secretName` created and
managed by the Certificate object. If you are manually importing your 
certificates into separate Secrets for each hostname, you can reference
these by instead using multiple `port` objects in your Gateway:

```yaml
- port:
    number: 443
    name: https
    protocol: HTTPS
  tls:
    mode: SIMPLE
    credentialName: "splunk-example-com-tls"
  hosts:
  - "splunk.example.com"
- port:
    number: 443
    name: https
    protocol: HTTPS
  tls:
    mode: SIMPLE
    credentialName: "deployer-splunk-example-com-tls"
  hosts:
  - "deployer.splunk.example.com"
...
```

Next, you will need to create VirtualServices for each of the components that you want to expose outside of Kubernetes:

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: splunk
spec:
  hosts:
  - "splunk.example.com"
  gateways:
  - "splunk-gw"
  http:
  - match:
    - uri:
        prefix: "/services/collector"
    route:
    - destination:
        port:
          number: 8088
        host: splunk-example-indexer-service
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-search-head-service
  tcp:
  - match:
    - port: 9997
    route:
    - destination:
        port:
          number: 9997
        host: splunk-example-indexer-service
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: splunk-deployer
spec:
  hosts:
  - "deployer.splunk.example.com"
  gateways:
  - "splunk-gw"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-deployer-service
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: splunk-cluster-master
spec:
  hosts:
  - "cluster-master.splunk.example.com"
  gateways:
  - "splunk-gw"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-cluster-master-service
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: splunk-license-master
spec:
  hosts:
  - "license-master.splunk.example.com"
  gateways:
  - "splunk-gw"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-license-master-service
```

Finally, you will need to create a DestinationRule to ensure user sessions are
sticky to specific search heads:

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: splunk-search-head-rule
spec:
  host: splunk-example-search-head-service
  trafficPolicy:
    loadBalancer:
      consistentHash:
        httpCookie:
          name: SPLUNK_ISTIO_SESSION
          ttl: 3600s
```
