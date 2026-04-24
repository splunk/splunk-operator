---
title: Ingress
parent: Deploy & Configure
nav_order: 4
---


# Configuring Ingress 

Using `port-forward` is great for testing, but you will ultimately want to make it easier to access your Splunk cluster outside of Kubernetes. A common approach is through the use of [Kubernetes Ingress Controllers](https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/).

There are many Ingress Controllers available, with each having their own pros and cons. There are just as many ways to configure each of them, which also depends upon your specific infrastructure and organizational policies. 

The Splunk Operator will automatically create and manage Kubernetes Services for all of the relevant components, and we expect these will provide for easy integration with most Ingress Controllers and configurations.

```
$ kubectl get services -o name
service/splunk-cluster-cluster-manager-service
service/splunk-cluster-deployer-service
service/splunk-cluster-indexer-headless
service/splunk-cluster-indexer-service
service/splunk-cluster-license-manager-service
service/splunk-cluster-search-head-headless
service/splunk-cluster-search-head-service
service/splunk-standalone-standalone-headless
service/splunk-standalone-standalone-service
```

We provide some examples below for configuring supported ingress approaches: [Gateway API](GatewayAPI.md) and [Istio](https://istio.io/). We hope these will serve as a useful starting point to configuring ingress in your environment.

* [Configuring Ingress Using Gateway API](GatewayAPI.md)
* [Configuring Ingress Using Istio](#Configuring-Ingress-Using-Istio)
* [Using Let's Encrypt to manage TLS certificates](#using-lets-encrypt-to-manage-tls-certificates)


Before deploying an example, you will need to review the yaml and replace “example.com” with the domain name you would like to use and replace “example” in the service names with the name of your custom resource object. You will also need to point your DNS for all the desired hostnames to the IP addresses of your ingress load balancer.


#### Important Notes on using Splunk on Kubernetes 

#### Load Balancer Requirements

When configuring ingress for use with Splunk Forwarders, the configured ingress load balancer must resolve to two or more IPs. This is required so the auto load balancing capability of the forwarders is preserved.

#### Splunk default network ports

When creating a new Splunk instance on Kubernetes, the default network ports will be used for internal communication such as internal logs, replication, and others. Any change in how these ports are configured needs to be consistent across the cluster. 

For Ingress we recommend using separate ports for encrypted and non-encrypted traffic. In this documentation we will use port 9998 for encrypted data coming from outside the cluster, while keeping the default 9997 for non-encrypted intra-cluster communication. For example, this  [ServiceTemplate configuration](#serviceTemplate) creates a standalone instance with port 9998 exposed.

#### Indexer Discovery is not supported
Indexer Discovery is not supported on a Kubernetes cluster. Instead, the Ingress controllers will be responsible to connect forwarders to peer nodes in Indexer clusters.

#### Sticky Sessions
When configuring the ingress configuration, it is important to ensure that the session is sticky to the Splunk specific instance. This is required for Splunk to work properly since otherwise, a blank page might be experienced when trying to access the Splunk instances. The examples below show how to configure this using Istio.


## Configuring Ingress Using Istio

Istio as an ingress controller allows the cluster to receive requests from external sources and routes them to a desired destination within the cluster. Istio utilizes an Envoy proxy that allows for precise control over how data is routed to services by looking at attributes such as, hostname, uri, and HTTP headers. Through the use of destination rules, it also allows for fine grain control over how data is routed even within services themselves. 

For instructions on how to install and configure Istio for your specific infrastructure, see the Istio [getting started guide](https://istio.io/docs/setup/getting-started/).

Most scenarios for Istio will require the configuration of a Gateway and a Virtual Service. Familiarize yourself with the [Istio Gateway](https://istio.io/latest/docs/reference/config/networking/gateway/) and [Istio Virtual Service ](https://istio.io/latest/docs/reference/config/networking/virtual-service/).

### Configuring Ingress for Splunk Web and HEC

You can configure Istio to provide direct access to Splunk Web.

#### Standalone Configuration

1. Create a Gateway to receive traffic on port 80

```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: splunk-web
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 80
      name: UI
      protocol: TCP
    hosts:
    - "splunk.example.com"
```

2. Create a virtual service to route traffic to your service. In this example we use a standalone:

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-web
spec:
  hosts:
  - "splunk.example.com"
  gateways:
  - "splunk-web"
  tcp:
  - match:
    - port: 80
    route:
    - destination:
        port:
          number: 8000
        host: splunk-standalone-standalone-service
```

3. Get the External-IP for Istio using the command:

```shell
kubectl get svc -n istio-system
```

4. Use a browser to connect to the External-IP to access Splunk Web. For example:

```
http://<LoadBalancer-External-IP>
```

#### Multiple Hosts and HEC Configuration

If your deployment has multiple hosts such as Search Heads and Cluster Manager, use this example to configure Splunk Web access, and HTTP Event Collector port. Follow the steps here [HEC Documentation](https://docs.splunk.com/Documentation/Splunk/latest/Data/UsetheHTTPEventCollector) to learn how to create a HEC token and how to send data using HTTP. 

1. Create a Gateway for multiple hosts.

```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: splunk-web
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
    - "cluster-manager.splunk.example.com"
    - "license-manager.splunk.example.com"
```

2. Create a VirtualService for each of the components that you want to expose outside of Kubernetes:

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-web
spec:
  hosts:
  - "splunk.example.com"
  gateways:
  - "splunk-web"
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
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-deployer
spec:
  hosts:
  - "deployer.splunk.example.com"
  gateways:
  - "splunk-web"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-deployer-service
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-cluster-manager
spec:
  hosts:
  - "cluster-manager.splunk.example.com"
  gateways:
  - "splunk-web"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-cluster-manager-service
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-license-manager
spec:
  hosts:
  - "license-manager.splunk.example.com"
  gateways:
  - "splunk-web"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-license-manager-service
```

3. Create a DestinationRule to ensure user sessions are sticky to specific search heads:

```yaml
apiVersion: networking.istio.io/v1beta1
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

If you are using HTTP Event Collector, modify your `ingress-gateway` service to listen for inbound TCP connections on port 8088.

```shell
$ kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-hec","port":8088,"protocol":"TCP"}]}}'
```

### Configuring Ingress for Splunk Forwarder data
The pre-requisites for enabling inbound communications from Splunk Forwarders to the cluster are configuring the Istio Gateway and Istio Virtual Service:

1. Create a Gateway:

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
      number: 9997
      name: tcp-s2s
      protocol: TCP
    hosts:
    - "splunk.example.com"
```

2. Create a Virtual Service:

```yaml
apiVersion: networking.istio.io/v1beta1
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

3. Modify your `ingress-gateway` service to listen for inbound TCP connections on port 9997:

```shell
$ kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-s2s","port":9997,"protocol":"TCP"}]}}'
```

4. Use the External-IP from Istio in the forwarder's outputs.conf.

```shell
kubectl get svc -n istio-system
```


### Configuring Ingress for Splunk Forwarder data with TLS
It is highly recommended that you always use TLS encryption for your Splunk Enterprise endpoints. The following sections will cover the two main configurations supported by Istio.

#### Splunk Forwarder data with end-to-end TLS
In this configuration Istio passes the encrypted traffic to Splunk Enterprise without any termination. Note that you need to configure the TLS certificates on the Forwarder as well as any Splunk Enterprise indexers, cluster peers, or standalone instances.



<img src="pictures/TLS-End-to-End.png?" alt="End-to-End Configuration" align="left" style="zoom:50%;" />

When using TLS for Ingress, we recommend you add an additional port for secure communication. By default, port 9997 will be assigned for non-encrypted traffic and you can use any other available port for secure communications. 

This example shows how to add port 9998 for a standalone instance:

```yaml
apiVersion: enterprise.splunk.com/v4
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
      - name: tls-splunktest
        port: 9998
        protocol: TCP
        targetPort: 9998
```

1. Modify your `ingress-gateway` Service to listen for S2S TCP connections on the new port created (9998):

```shell
$ kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-tls","port":9998,"protocol":"TCP"}]}}'
```

2. Create a Gateway with TLS Passthrough:

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

3. Create a Virtual Service for TLS routing:

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

*Note*: this TLS example requires that `outputs.conf` on your forwarders includes the setting `tlsHostname = splunk.example.com`. Istio requires the TLS header to be defined so it to know which indexers to forward the traffic to. If this parameter is not defined, your forwarder connections will fail.

If you only have one indexer cluster that you would like to use as the destination for all S2S traffic, you can optionally replace `splunk.example.com` in the above examples with the wildcard `*`. When you use this wildcard, you do not have to set the `tlsHostname` parameter in `outputs.conf` on your forwarders.

4. Deploy an app to the standalone instance with the inputs.conf settings needed to open port 9998 and configure the relevant TLS settings. For details on app management using the Splunk Operator, see [Using Apps for Splunk Configuration](https://splunk.github.io/splunk-operator/Examples.html#installing-splunk-apps).

Configure the Forwarder's outputs.conf and the Indexer's inputs.conf using the documentation [Configure Secure Forwarding](https://docs.splunk.com/Documentation/Splunk/latest/Security/Aboutsecuringdatafromforwarders)

#### Splunk Forwarder data with TLS Gateway Termination

In this configuration, Istio is terminating the encryption at the Gateway and forwarding the decrypted traffic to Splunk Enterprise. Note that in this case the Forwarder's outputs.conf should be configured for TLS, while the Indexer's input.conf should be configured to accept non-encrypted traffic.



<img src="pictures/TLS-Gateway-Termination.png?" alt="End-to-End Configuration" align="left" style="zoom:50%;" />


1. Create a TLS secret with the certificates needed to decrypt traffic. These are the same commands used on your Indexer to terminate TLS:

```shell
kubectl create -n istio-system secret tls s2s-tls --key=<Path to private key> --cert=<Path to Indexer certificate>
```

2. Create a Gateway that terminates TLS:

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
      number: 9997
      name: tls-s2s
      protocol: TLS
    tls:
      mode: SIMPLE
      credentialName: s2s-tls # must be the same as secret
    hosts:
    - "*"
```

3. Create a Virtual Service for TCP routing:

```yaml
apiVersion: networking.istio.io/v1beta1
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
    - port: 9997
    route:
    - destination:
        port:
          number: 9997
        host: splunk-standalone-standalone-service
```
Note that the Virtual Service no longer handles TLS since it has been terminated at the gateway.

4. Configure your Forwarder and Indexer or Standalone certificates using the documentation: [Securing data from forwarders](https://docs.splunk.com/Documentation/Splunk/latest/Security/Aboutsecuringdatafromforwarders). 

##### Documentation tested on Istio v1.8 and Kubernetes v1.17

### Sticky Sessions
Follow [Istio Sticky Sessions Documentation](https://istio.io/latest/docs/reference/config/networking/destination-rule/#LoadBalancerSettings) to learn how to configure session stickiness for Istio.

## Note on Service Mesh and Istio

Istio is a popular choice for its Service Mesh capabilities. However, Service Mesh for Splunk instances are only supported on Istio v1.8 and above, along with Kubernetes v1.19 and above. At the time of this documentation, neither Amazon AWS nor Google Cloud have updated their stack to these versions.

## Using Let's Encrypt to manage TLS certificates

If you are using [cert-manager](https://docs.cert-manager.io/en/latest/getting-started/) with [Let’s Encrypt](https://letsencrypt.org/) to manage your TLS certificates in Kubernetes, this example Ingress object can be used to enable secure (TLS) access to all Splunk components from outside of your Kubernetes cluster:

### Example configuration for Istio


If you are using [cert-manager](https://docs.cert-manager.io/en/latest/getting-started/) with [Let’s Encrypt](https://letsencrypt.org/) to manage your TLS certificates in Kubernetes:

1. Create the Certificate object and populate a `splunk-example-com-tls` secret in the `istio-system` namespace. For example:

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
    - cluster-manager.splunk.example.com
    - license-manager.splunk.example.com
  issuerRef:
    name: letsencrypt-prod
    kind: ClusterIssuer
```

2. Create an Istio Gateway that is associated with your certificates:

```yaml
apiVersion: networking.istio.io/v1beta1
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
    - "cluster-manager.splunk.example.com"
    - "license-manager.splunk.example.com"
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
    - "cluster-manager.splunk.example.com"
    - "license-manager.splunk.example.com"
```

Note that the `credentialName` references the same `secretName` created and managed by the Certificate object. 

3. If you are manually importing your certificates into separate Secrets for each hostname, you can reference these by instead using multiple `port` objects in your Gateway:

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

4. Create VirtualServices for each of the components that you want to expose outside of Kubernetes:

```yaml
apiVersion: networking.istio.io/v1beta1
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
apiVersion: networking.istio.io/v1beta1
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
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-cluster-manager
spec:
  hosts:
  - "cluster-manager.splunk.example.com"
  gateways:
  - "splunk-gw"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-cluster-manager-service
---
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: splunk-license-manager
spec:
  hosts:
  - "license-manager.splunk.example.com"
  gateways:
  - "splunk-gw"
  http:
  - route:
    - destination:
        port:
          number: 8000
        host: splunk-example-license-manager-service
```

5. Create a DestinationRule to ensure user sessions are sticky to specific search heads:

```yaml
apiVersion: networking.istio.io/v1beta1
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
