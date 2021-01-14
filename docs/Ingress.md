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
service/splunk-standalone-standalone-service
```

*[To-Review] Please note that services are currently only created for managed clusters. No
services will be created for single instance deployments.*

Below we provide some examples for configuring two of the most popular Ingress controllers: [Istio](https://istio.io/) and the
[NGINX Ingress Controller](https://www.nginx.com/products/nginx/kubernetes-ingress-controller). We hope these will serve as a useful starting
point to configuring ingress in your particular environment.

Before deploying an example, you will need to replace “example.com” with
whatever domain name you would like to use, and “example” in the service
names with the name of your custom resource object. You will also need
to point your DNS for all the desired hostnames to the IP addresses of 
your ingress load balancer.


[Change #1 - Start with Istio since it's the preferable method]
## Example: Configuring Ingress Using Istio

Istio as an ingress controller allows us to receive requests from external sources and route them to a desired destination within the Kubernetes cluster. Behind the scenes, Istio configures an Envoy proxy that allows for precise control over how data is routed to services by looking at attributes such as, hostname, uri, and HTTP headers. Through destination rules, it also allows for fine grain control over how data is routed even within services themselves. 

For instructions on how to install and configure Istio for your specific
infrastructure, please see its
[getting started guide](https://istio.io/docs/setup/getting-started/).

[Change #2 - Include overview]
## Main Components

### Gateway

 [Gateway](https://istio.io/latest/docs/reference/config/networking/gateway/)

The istio gateway describes a load balancer sitting at the edge of the cluster and serves as the entry point for external traffic into the Kubernetes cluster. The gateway is configured with the specific port and protocol combinations that data will be sent to on the load balancer. How the traffic is routed to services is handled later by the Virtual Service configuration.  
[Ingress Control](https://istio.io/latest/docs/tasks/traffic-management/ingress/ingress-control/) 

[Ingress Gateway](https://istio.io/latest/docs/examples/microservices-istio/istio-ingress-gateway/)


### Virtual Service

[Virtual Service](https://istio.io/latest/docs/reference/config/networking/virtual-service/)

The Virtual Service configuration describes to Istio's Envoy proxy how traffic received via a specified gateway should be routed to specific services within the Kubernetes cluster. Attributes used to route traffic include hostname, URI, and HTTP header information. Without a virtual service configuration the Envoy proxy would default to round robin load balancing across all services.


### Destination Rule

[Destination Rule](https://istio.io/latest/docs/reference/config/networking/destination-rule/)

Destination rules help direct traffic after service routing has occurred. They can change the load balancing of requests to a service, and even define how traffic is routed to subsets of a service. Destination rules are not required for Ingress. 

[Destination Rule Example](https://istio.io/latest/docs/concepts/traffic-management/#destination-rule-example)

### Sidecar Injection

In addition to just being used as an ingress controller, Istio has a much deeper feature set if Istio sidecars are injected into Splunk Operator pods. 

When the Istio sidecar is present Istio will intercept all traffic between services in the cluster and through the Istio control plane, allow for fine tuning of traffic flowing through the environment. 

Available features include:

- Automatic load balancing for HTTP, gRPC, WebSocket, and TCP traffic.
- Fine-grained control of traffic behavior with rich routing rules, retries, failovers, and fault injection.
- A pluggable policy layer and configuration API supporting access controls, rate limits and quotas.
- Automatic metrics, logs, and traces for all traffic within a cluster, including cluster ingress and egress.
- Secure service-to-service communication in a cluster with strong identity-based authentication and authorization.

#### Configuring  Ingress for Splunk (S2S)

Create a gateway
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: splunk-s2s
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 9997
      name: tcp-s2s
      protocol: TCP
    hosts:
    - "splunk.example.com"
```

Create  Virtual Service
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: splunk-s2s
spec:
  hosts:
  - "splunk.example.com"
  gateways:
  - "splunk-s2s"
  tcp:
  - match:
    - port: 9997
    route:
    - destination:
        port:
          number: 9997
        host: splunk-example-indexer-service
```

Modify your `ingress-gateway` Service to listen for S2S TCP
connections on port 9997.

```shell
$ kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-s2s","port":9997,"protocol":"TCP"}]}}'   service/istio-ingressgateway patched
```

Use the External-IP from Istio in the Forwarder's outputs.conf.
```shell
kubectl get svc -n istio-system
```

It is highly recommended that you always use TLS encryption for your Splunk
endpoints. There are two main configurations supported by Istio. First is the passthrough configuration (End-to-End) which  terminates the encryption in the pod level. The second is TLS Termination at Gateway, in which Istio validates and decrypts the data prior to sending it to the pods. 

[To-Discuss: Are these pictures a good fit here?]

 https://confluence.splunk.com/pages/viewpage.action?pageId=412881933&preview=/412881933/435214418/image2021-1-11_14-1-46.png

and 
https://confluence.splunk.com/pages/viewpage.action?pageId=412881933&preview=/412881933/435214423/image2021-1-11_14-6-31.png



Learn how to manage TLS certificates to secure data from Forwarders:  
[Securing Splunk Platform](https://docs.splunk.com/Documentation/Splunk/8.1.1/Security/Aboutsecuringdatafromforwarders)



#### Configuring  Ingress for Splunk (S2S) with End-to-End TLS

When using TLS for Ingress we recommend you to use an additional port for secure communication. By default port 9997 will be assigned for non-encrypted traffic, you can use any other available port for secure communication. This example shows how to add port 9998 for a standalone instance.

```yaml
apiVersion: enterprise.splunk.com/v1beta1
kind: Standalone
metadata:
  name: standalone
  labels:
    app: SplunkStandAlone
    type: Splunk
  finalizers:
  - enterprise.splunk.com/delete-pvc
spec:
  serviceTemplate:
    spec:
      ports:
      - name: splunktest
        port: 9998
        protocol: TCP
        targetPort: 9998
```

Modify your `ingress-gateway` Service to listen for S2S TCP
connections on the new port created (9998).
```shell
$ kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-tls","port":9998,"protocol":"TCP"}]}}'   service/istio-ingressgateway patched
```

Create a Gateway with TLS Passthrough
```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: splunk-s2s
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 9998
      name: tls-s2s
      protocol: TLS
    tls:
      mode: PASSTHROUGH
    hosts:
    - "*"
```

Create a Virtual Service for TLS routing
```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-s2s
spec:
  hosts:
  - "*"
  gateways:
  - "splunk-s2s"
  tls:
  - match:
    - port: 9998
      sniHosts:
      - "splunk.example.com"
    route:
    - destination:
        host: splunk-standalone-standalone-service
        port:
          number: 9998
```

*Please note*: this TLS example requires that `outputs.conf` on your forwarders
includes the parameter `tlsHostname = splunk.example.com`. Istio requires this
TLS header to be defined for it to know which indexers to forward the traffic
to. If this parameter is not defined, your forwarder connections will fail.

If you only have one indexer cluster that you would like to use for all S2S
traffic, you can optionally replace `splunk.example.com` in the above examples
with the wildcard `*`. When you use this wildcard, you do not have to set the
`tlsHostname` parameter in `outputs.conf` on your forwarders.

Configure Forwarder's outputs.conf  for TLS
```
[tcpout]
defaultGroup = default-autolb-group
useSSL = true
 
[tcpout:default-autolb-group]
disabled = false
server = <Host>:<Port>
sslRootCAPath = <Path to your CA Certificate>
clientCert =  <Path to your Forwarder Certificate>
tlsHostname = splunk.example.com
 
[tcpout-server://<Host>:<Port>]
```
More details: [Outputs.conf Docs](https://docs.splunk.com/Documentation/Splunk/8.1.1/Admin/Outputsconf)

Configure Indexer's Inputs.conf for TLS
```
[splunktcp-ssl:9998]
 
[SSL]
serverCert= <Path to your Indexer Certificate>
sslRootCAPath = <Path to your CA Certificate>
```

More details: [Inputs.conf Docs](https://docs.splunk.com/Documentation/Splunk/8.1.1/Admin/Inputsconf)



#### Configuring  Ingress for Splunk (S2S) with TLS Gateway Termination

For this configuration we need to create a TLS secret with the certificates needed to decrypt traffic. These are the same you'd use in your Indexer to terminate TLS.

```shell
kubectl create -n istio-system secret tls s2s-tls --key=<Path to private key> --cert=<Path to Indexer certificate>
```

Create a Gateway that terminates TLS
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: splunk-s2s
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 9998
      name: tls-s2s
      protocol: TLS
    tls:
      mode: SIMPLE
      credentialName: s2s-tls # must be the same as secret
    hosts:
    - "*"
```

Create a Virtual Service for TCP routing. 
```yaml
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: splunk-s2s
spec:
  hosts:
  - "*"
  gateways:
  - splunk-s2s
  tcp:
  - match:
    - port: 9998
    route:
    - destination:
        port:
          number: 9998
        host: splunk-standalone-standalone-service
```
Note the virtual service no longer handles TLS since it has been terminated at the gateway.


There is no change in the Forwarder's configuration as the previous example [link]. For the Indexer however the inputs.conf needs to be updated to receive non-encrypted traffic.

Configure Indexer's Inputs.conf for TCP
```shell
[splunktcp://9998]
disabled = 0
```

#### Configuring Web UI access using Istio 

[In-progress]









[To Discuss - Remove section on Let's encrypt or leave it?]

It is highly recommended that you always use TLS encryption for your Splunk
-endpoints. To do this, you will need to have one or more Kubernetes TLS
-Secrets for all the hostnames you want to use with Splunk deployments.To do this, you will need to have one or more Kubernetes TLS Secrets for all the hostnames you want to use with Splunk deployments. Note that these secrets must reside in the same namespace as your Istio Ingress pod, most likely `istio-system`.

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



[TOBE UPDATED:]
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


