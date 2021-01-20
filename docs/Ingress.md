# Configuring Ingress

Using `port-forward` is great for testing, but you will ultimately want to
make it easier to access your Splunk cluster outside of Kubernetes. A common 
approach is using [Kubernetes Ingress Controllers](https://kubernetes.io/docs/concepts/services-networking/ingress-controllers/).

There are many Ingress Controllers available, with each having their own pros and cons. There are just as many ways to configure each of them, which also depends upon your specific infrastructure and organizational policies. Splunk Operator will automatically create and manage Kubernetes Services for all of the relevant components, and we expect these will provide for easy integration with most Ingress Controllers and configurations.

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

We provide some examples below for configuring a few of the most popular Ingress controllers: [Istio](https://istio.io/) , [Nginx-inc](https://docs.nginx.com/nginx-ingress-controller/overview/) and [Ingress Nginx](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration). We hope these will serve as a useful starting point to configuring ingress in your environment.

Before deploying an example, you will need to review the yaml and replace “example.com” with the domain name you would like to use, and replace “example” in the service names with the name of your custom resource object. You will also need to point your DNS for all the desired hostnames to the IP addresses of your ingress load balancer.


## Configuring Ingress Using Istio

Istio as an ingress controller allows the cluster to receive requests from external sources and routes them to a desired destination within the cluster. Istio utilizes an Envoy proxy that allows for precise control over how data is routed to services by looking at attributes such as, hostname, uri, and HTTP headers. Through the use of destination rules, it also allows for fine grain control over how data is routed even within services themselves. 

For instructions on how to install and configure Istio for your specific infrastructure, see the Istio [getting started guide](https://istio.io/docs/setup/getting-started/).

### Configuring Ingress for Splunk Web 

You can configure Istio to provide direct access to Splunk Web.

Create a Gateway to receive traffic on port 80

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
      number: 80
      name: UI
      protocol: TCP
    hosts:
    - "splunk.example.com"
```

Create a virtual service to route traffic to your service, in this example we used a standalone.
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
    - port: 80
    route:
    - destination:
        port:
          number: 8000
        host: splunk-standalone-standalone-service
```

Get the External-IP for Istio using the command:
```shell
kubectl get svc -n istio-system
```

On your browser, use the External-IP to access Splunk Web, for example:
```
http://<LoadBalance-External-IP>
```


### Configuring Ingress for Splunk Forwarder data
The pre-requisites for enabling inbound communications from Splunk Forwarders to the cluster are configuring the Istio Gateway and Istio Virtual Service:

#### Gateway

The [Istio Gateway](https://istio.io/latest/docs/reference/config/networking/gateway/) defines a load balancer at the edge of the cluster that serves as the entry point for external traffic into the Kubernetes cluster. The gateway is configured with the specific port and protocol combinations that data will be sent to on the load balancer. How the traffic is routed to services is handled by the Virtual Service configuration.

Example Gateway configuration
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

#### Virtual Service

The [Istio Virtual Service](https://istio.io/latest/docs/reference/config/networking/virtual-service/) configuration describes to Istio's Envoy proxy how traffic received on a the gateway should be routed to specific services within the Kubernetes cluster. Attributes used to route traffic include hostname, URI, and HTTP header information. 

Example Virtual Service Configuration
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

Modify your `ingress-gateway` service to listen for inbound TCP connections on port 9997.

```shell
$ kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-s2s","port":9997,"protocol":"TCP"}]}}'
```

Use the External-IP from Istio in the Forwarder's outputs.conf.
```shell
kubectl get svc -n istio-system
```


### Configuring Ingress for Splunk Forwarder data with TLS
It is highly recommended that you always use TLS encryption for your Splunk Enterprise endpoints. The following sections will cover the two main configurations supported by Istio.

#### Splunk Forwarder data with end-to-end TLS
In this configuration Istio passes the encrypted traffic to Splunk Enterprise without any termination. Note that you need to configure the TLS certificates on the Forwarder as well as any Splunk Enterprise indexers, cluter peers, or standalone instances.

<img src="pictures/TLS-End-to-End.png?" alt="End-to-End Configuration" align="left" style="zoom:50%;" />

When using TLS for Ingress, we recommend you to add an additional port for secure communication. By default, port 9997 will be assigned for non-encrypted traffic and you can use any other available port for the secure communications. This example shows how to add port 9998 for a standalone instance.

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
$ kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-tls","port":9998,"protocol":"TCP"}]}}'
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

*Note*: this TLS example requires that `outputs.conf` on your forwarders includes the setting `tlsHostname = splunk.example.com`. Istio requires the TLS header to be defined so it to know which indexers to forward the traffic to. If this parameter is not defined, your forwarder connections will fail.

If you only have one indexer cluster that you would like to use as the destination for all S2S traffic, you can optionally replace `splunk.example.com` in the above examples with the wildcard `*`. When you use this wildcard, you do not have to set the `tlsHostname` parameter in `outputs.conf` on your forwarders.

Configure the Forwarder's outputs.conf and the Indexer's inputs.conf using the documentation [Configure Secure Forwarding](https://docs.splunk.com/Documentation/Splunk/latest/Security/Aboutsecuringdatafromforwarders)

#### Splunk Forwarder data with TLS Gateway Termination

In this configuration, Istio is terminating the encryption at the Gateway and forwarding the decrypted traffic to Splunk Enterprise. Note that in this case the Forwarder's outputs.conf should be configured for TLS, while the Indexer's input.conf should be configured to accept non-encrypted traffic.

<img src="pictures/TLS-Gateway-Termination.png?" alt="End-to-End Configuration" align="left" style="zoom:50%;" />


Create a TLS secret with the certificates needed to decrypt traffic. These are the same commands used on your Indexer to terminate TLS.

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
Note that the Virtual Service no longer handles TLS since it has been terminated at the gateway.

Configure your Forwarder and Indexer or Standalone certificates using the documentation: [Securing data from forwarders](https://docs.splunk.com/Documentation/Splunk/latest/Security/Aboutsecuringdatafromforwarders). 


## Configuring Ingress Using NGINX

###  Before We Begin

**NOTE**: There are at least 3 flavors of the Nginx ingress controller.

- Kubernetes Ingress Nginx (open source)
- Nginx Ingress Open Source (F5's open source version)
- Nginx Ingress Plus (F5's paid version)

[Nginx Comparison Chart](https://github.com/nginxinc/kubernetes-ingress/blob/master/docs/nginx-ingress-controllers.md).

This is very important since they look the same at first, but have *very* different annotations and configurations. For this doc, we are using two flavors, the open source version (option 1) and F5 Opens source version of Nginx Ingress (option 2).


## Example: Configuring Ingress Using Ingress Nginx

For instructions on how to install and configure the NGINX Ingress Controller
for your specific infrastructure, please see its
[GitHub repository](https://github.com/nginxinc/kubernetes-ingress/) and [Installation Guide](https://kubernetes.github.io/ingress-nginx/deploy/).

The configurations for creating and managing your certificates, as well as the Forwarder and Indexer's configurations are the same as the example above for Istio End-to-End TLS.

This Ingress Controller uses a ConfigMap to enable Ingress access to the cluster. Currently there is no support for TCP gateway termination. Requests for this feature have been placed such as in  [Request 3087](https://github.com/kubernetes/ingress-nginx/issues/3087) and [Ticket 636](https://github.com/kubernetes/ingress-nginx/issues/636) but at the time of this documentation, only HTTPS is supported for gateway termination.

For Splunk-S2S communication which is based on TCP the only configuration available is End-to-End termination. 

#### Configuring Ingress-NGINX for Splunk (S2S) with End-to-End TLS

We used this base Yaml provided in the Installation Guide for AWS as a template: 
[AWS Sample Deployment](https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v0.43.0/deploy/static/provider/aws/deploy.yaml)

Next we will configure the ports we want to use for Ingress. In the following example we used the ports 9997 for non-encryption and 9998 for encryption.

Use the default configuration with the following additions:
###### 1) Add a configMap to define the port-to-service routing
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tcp-services
  namespace: ingress-nginx
data:
  9997: "default/splunk-standalone-standalone-service:9997"
  9998: "default/splunk-standalone-standalone-service:9998"
```

###### 2) Add the two ports into the Service used to configure the Load Balancer
```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-backend-protocol: tcp
    service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: 'true'
    service.beta.kubernetes.io/aws-load-balancer-type: nlb
  labels:
    helm.sh/chart: ingress-nginx-3.10.1
    app.kubernetes.io/name: ingress-nginx
    app.kubernetes.io/instance: ingress-nginx
    app.kubernetes.io/version: 0.41.2
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/component: controller
  name: ingress-nginx-controller
  namespace: ingress-nginx
spec:
  type: LoadBalancer
  externalTrafficPolicy: Local
  ports:
    - name: http
      port: 80
      protocol: TCP
      targetPort: http
    - name: https
      port: 443
      protocol: TCP
      targetPort: https
    - name: tcp-s2s
      port: 9997
      protocol: TCP
      targetPort: 9997
    - name: tls-s2s
      port: 9998
      protocol: TCP
      targetPort: 9998
```



## Example: Configuring Ingress Using NGINX-Ingress-Controller (Nginxinc)

Nginx Ingress Controller is an open source version supported by F5. Please review the official documentation below for more details.

[Github Repo](https://github.com/nginxinc/kubernetes-ingress)

[Docs Home](https://docs.nginx.com/nginx-ingress-controller/overview/)

[Annotations Page](https://docs.nginx.com/nginx-ingress-controller/configuration/ingress-resources/advanced-configuration-with-annotations/)


#### Installing the Nginx Helm Chart
We followed this guide for installing the Helm Chart for the ingress controller. This assumes that the cluster has internet access.

[Helm Installation page]( https://docs.nginx.com/nginx-ingress-controller/installation/installation-with-helm/)

#### Helm Setup

```shell
# clone the repo and check out the current production branch
$ git clone https://github.com/nginxinc/kubernetes-ingress/
$ cd kubernetes-ingress/deployments/helm-chart
$ git checkout v1.9.0

# add the helm chart
$ helm repo add nginx-stable https://helm.nginx.com/stable
$ helm repo update

# install custom resource definitions
$ kubectl create -f crds/
```

#### Ingress Install
```shell
cd deployments/helm-chart

# Edit and make changes to values.yaml as needed
helm install epat-eks-nginx nginx-stable/nginx-ingress

#list the helms installed
helm list

NAME            NAMESPACE   REVISION    UPDATED             STATUS      CHART               APP VERSION
epat-eks-nginx  default     5  2020-10-29 15:03:47.6 EDT    deployed    nginx-ingress-0.7.0 1.9.0

#if needed to update any configs for ingress, update the values.yaml and run upgrade
helm upgrade epat-eks-nginx  nginx-stable/nginx-ingress
```

#### Configuring Ingress

##### Ingress Service

The following ingress example yaml exposes Splunk HEC and UI for an operator installed service. HEC is exposed via ssl and UI is non-ssl.
```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  annotations:
    certmanager.k8s.io/cluster-issuer: letsencrypt-prod
    nginx.org/client-body-buffer-size: 100M
    nginx.org/client-max-body-size: "0"
    nginx.org/server-snippets: |
      client_body_buffer_size 100m;
    nginx.org/ssl-services: splunk-epat-smartstore-standalone-headless
  name: splunk-ingress
  namespace: default
spec:
  ingressClassName: nginx
  rules:
  - host: s1.operator.splunkepat.com
    http:
      paths:
      - backend:
          serviceName: splunk-epat-smartstore-standalone-service
          servicePort: 8000
        path: /en-US
        pathType: Prefix
      - backend:
          serviceName: splunk-epat-smartstore-standalone-headless
          servicePort: 8088
        path: /services/collector
        pathType: Prefix
      - backend:
          serviceName: cm-acme-http-solver-5gcx9
          servicePort: 8089
        path: /.well-known
        pathType: Prefix
  tls:
  - hosts:
    - s1.operator.splunkepat.com
    secretName: operator-tls
status:
  loadBalancer: {}
```

##### S2S Setup
In order to expose s2s, we need to enable the global configuration to setup a listener and transport server

```yaml
apiVersion: k8s.nginx.org/v1alpha1
kind: GlobalConfiguration
metadata:
  name: nginx-configuration
  namespace: default
spec:
  listeners:
  - name: s2s-tcp
    port: 30403
    protocol: TCP
apiVersion: k8s.nginx.org/v1alpha1
kind: TransportServer
metadata:
  name: s2s-tcp
spec:
  listener:
    name: s2s-tcp
    protocol: TCP
  upstreams:
  - name: s2s-app
    service: splunk-epat-smartstore-standalone-service
    port: 9997
      action:
    pass: s2s-app
```

We need to edit the service to setup a node-port for the port being setup for the listener

List the service
```yaml
kubectl get svc
NAME                                         TYPE           CLUSTER-IP       EXTERNAL-IP                                                               PORT(S)                                                            AGE
epat-eks-nginx-nginx-ingress                 LoadBalancer   172.20.195.54    aa725344587a4443b97c614c6c78419c-1675645062.us-east-2.elb.amazonaws.com   80:31452/TCP,443:30402/TCP,30403:30403/TCP                         7d1h
```

Edit the service and add s2s ingress port
```
kubectl edit service epat-eks-nginx-nginx-ingress
```

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    meta.helm.sh/release-name: epat-eks-nginx
    meta.helm.sh/release-namespace: default
  creationTimestamp: "2020-10-23T17:05:08Z"
  finalizers:
  - service.kubernetes.io/load-balancer-cleanup
  labels:
    app.kubernetes.io/instance: epat-eks-nginx
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: epat-eks-nginx-nginx-ingress
    helm.sh/chart: nginx-ingress-0.7.0
  name: epat-eks-nginx-nginx-ingress
  namespace: default
  resourceVersion: "3295579"
  selfLink: /api/v1/namespaces/default/services/epat-eks-nginx-nginx-ingress
  uid: a7253445-87a4-443b-97c6-14c6c78419c9
spec:
  clusterIP: 172.20.195.54
  externalTrafficPolicy: Local
  healthCheckNodePort: 32739
  ports:
  - name: http
    nodePort: 31452
    port: 80
    protocol: TCP
    targetPort: 80
  - name: https
    nodePort: 30402
    port: 443
    protocol: TCP
    targetPort: 443
  - name: s2s
    nodePort: 30403
    port: 30403
    protocol: TCP
    targetPort: 30403
  selector:
    app: epat-eks-nginx-nginx-ingress
  sessionAffinity: None
  type: LoadBalancer
```

  

## Example using Let's Encrypt

If you are using [cert-manager](https://docs.cert-manager.io/en/latest/getting-started/)
with [Let’s Encrypt](https://letsencrypt.org/) to manage your TLS
certificates in Kubernetes, the following example Ingress object can be
used to enable secure (TLS) access to all Splunk components from outside of
your Kubernetes cluster:

##### 1) Sample configuration for  Nginx

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

##### 2) Sample configuration for Istio

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
