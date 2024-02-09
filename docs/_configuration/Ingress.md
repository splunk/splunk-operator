---
title: Ingress
nav_order: 25
#nav_exclude: true
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

We provide some examples below for configuring a few of the most popular Ingress controllers: [Istio](https://istio.io/) , [Nginx-inc](https://docs.nginx.com/nginx-ingress-controller/overview/) and [Ingress Nginx](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration). We hope these will serve as a useful starting point to configuring ingress in your environment.

* [Configuring Ingress Using Istio](#Configuring-Ingress-Using-Istio)
* [Configuring Ingress Using NGINX](#Configuring-Ingress-Using-NGINX)
* [Using Let's Encrypt to manage TLS certificates ](#installing-the-splunk-operator)


Before deploying an example, you will need to review the yaml and replace “example.com” with the domain name you would like to use and replace “example” in the service names with the name of your custom resource object. You will also need to point your DNS for all the desired hostnames to the IP addresses of your ingress load balancer.


### Important Notes on using Splunk on Kubernetes 

##### Load Balancer Requirements

When configuring ingress for use with Splunk Forwarders, the configured ingress load balancer must resolve to two or more IPs. This is required so the auto load balancing capability of the forwarders is preserved.

##### Splunk default network ports

When creating a new Splunk instance on Kubernetes, the default network ports will be used for internal communication such as internal logs, replication, and others. Any change in how these ports are configured needs to be consistent across the cluster. 

For Ingress we recommend using separate ports for encrypted and non-encrypted traffic. In this documentation we will use port 9998 for encrypted data coming from outside the cluster, while keeping the default 9997 for non-encrypted intra-cluster communication. For example, this  [ServiceTemplate configuration](#serviceTemplate) creates a standalone instance with port 9998 exposed.

##### Indexer Discovery is not supported
Indexer Discovery is not supported on a Kubernetes cluster. Instead, the Ingress controllers will be responsible to connect forwarders to peer nodes in Indexer clusters.


## Configuring Ingress Using Istio

Istio as an ingress controller allows the cluster to receive requests from external sources and routes them to a desired destination within the cluster. Istio utilizes an Envoy proxy that allows for precise control over how data is routed to services by looking at attributes such as, hostname, uri, and HTTP headers. Through the use of destination rules, it also allows for fine grain control over how data is routed even within services themselves. 

For instructions on how to install and configure Istio for your specific infrastructure, see the Istio [getting started guide](https://istio.io/docs/setup/getting-started/).

Most scenarios for Istio will require the configuration of a Gateway and a Virtual Service. Familiarize yourself with the [Istio Gateway](https://istio.io/latest/docs/reference/config/networking/gateway/) and [Istio Virtual Service ](https://istio.io/latest/docs/reference/config/networking/virtual-service/).

### Configuring Ingress for Splunk Web and HEC

You can configure Istio to provide direct access to Splunk Web.

##### Standalone Configuration

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

##### Multiple Hosts and HEC Configuration

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

##### Splunk Forwarder data with end-to-end TLS
In this configuration Istio passes the encrypted traffic to Splunk Enterprise without any termination. Note that you need to configure the TLS certificates on the Forwarder as well as any Splunk Enterprise indexers, cluster peers, or standalone instances.



<img src="/assets/images/TLS-End-to-End.png?" alt="End-to-End Configuration" align="left" style="zoom:50%;" />

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

##### Splunk Forwarder data with TLS Gateway Termination

In this configuration, Istio is terminating the encryption at the Gateway and forwarding the decrypted traffic to Splunk Enterprise. Note that in this case the Forwarder's outputs.conf should be configured for TLS, while the Indexer's input.conf should be configured to accept non-encrypted traffic.



<img src="/assets/images/TLS-Gateway-Termination.png?" alt="End-to-End Configuration" align="left" style="zoom:50%;" />


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

## Note on Service Mesh and Istio

Istio is a popular choice for its Service Mesh capabilities. However, Service Mesh for Splunk instances are only supported on Istio v1.8 and above, along with Kubernetes v1.19 and above. At the time of this documentation, neither Amazon AWS nor Google Cloud have updated their stack to these versions.

## Configuring Ingress Using NGINX

**NOTE**: There are at least 3 flavors of the Nginx Ingress controller.

- Kubernetes Ingress Nginx (open source)
- Nginx Ingress Open Source (F5's open source version)
- Nginx Ingress Plus (F5's paid version)

[Nginx Comparison Chart](https://github.com/nginxinc/kubernetes-ingress/blob/main/docs/content/intro/nginx-ingress-controllers.md).

It is important to confirm which Nginx Ingress controller you intended to implement, as they have *very* different annotations and configurations. For these examples, we are using the Kubernetes Ingress Nginx (option 1) and the Nginx Ingress Open Source (option 2).

## Configuring Ingress Using Kubernetes Ingress Nginx 

For instructions on how to install and configure the NGINX Ingress Controller, see the 
[NGINX Ingress Controller GitHub repository](https://github.com/kubernetes/ingress-nginx) and the [Installation Guide](https://kubernetes.github.io/ingress-nginx/deploy/).

This Ingress Controller uses a ConfigMap to enable Ingress access to the cluster. Currently there is no support for TCP gateway termination. Requests for this feature are available in [Request 3087](https://github.com/kubernetes/ingress-nginx/issues/3087), and [Ticket 636](https://github.com/kubernetes/ingress-nginx/issues/636) but at this time only HTTPS is supported for gateway termination.

For Splunk Forwarder communications over TCP, the only configuration available is End-to-End TLS termination. The details for creating and managing certificates, as well as the Forwarder and Indexer's configurations are the same as the example for Istio End-to-End TLS above.

For all configurations below, we started with the standard yaml provided in the Installation Guide for AWS as a template: 
[Ingress NGINX AWS Sample Deployment](https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v0.43.0/deploy/static/provider/aws/deploy.yaml). Then we will add or update those components based on each scenario. 

### Configuring Ingress for Splunk Web 

You can configure Nginx to provide direct access to Splunk Web.

Example to create the Ingress configuration for a standalone:

```yaml
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: ingress-standalone
  annotations:
    # use the shared ingress-nginx
    kubernetes.io/ingress.class: "nginx"
    nginx.ingress.kubernetes.io/default-backend: splunk-standalone-standalone-service
    nginx.ingress.kubernetes.io/proxy-body-size: "0"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "600"
spec:
  rules:
  - host: splunk.example.com
    http:
      paths:
      - path: /
        backend:
          serviceName: splunk-standalone-standalone-service
          servicePort: 8000
```



Example to create an Ingress configuration for multiple hosts:

```yaml
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: ingress-standalone
  annotations:
    # use the shared ingress-nginx
    kubernetes.io/ingress.class: "nginx"
    nginx.ingress.kubernetes.io/default-backend: splunk-standalone-standalone-service
    nginx.ingress.kubernetes.io/proxy-body-size: "0"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "600"
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
  - host: cluster-manager.splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-example-cluster-manager-service
          servicePort: 8000
```

Example to create a TLS enabled Ingress configuration

There are a few important configurations to be aware of in the TLS configuration:

* The `nginx.ingress.kubernetes.io/backend-protocol:` annotation requires `"HTTPS"` when TLS is configured on the backend services.
* The secretName must reference a [valid TLS secret](https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets).

Note: This example assumes that https is enabled for Splunk Web.

```
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: "nginx"
    nginx.ingress.kubernetes.io/affinity: "cookie"
    nginx.ingress.kubernetes.io/affinity-mode: "persistent"
    nginx.ingress.kubernetes.io/session-cookie-name: "route"
    nginx.ingress.kubernetes.io/session-cookie-expires: "172800"
    nginx.ingress.kubernetes.io/session-cookie-max-age: "172800"
    nginx.ingress.kubernetes.io/client-body-buffer-size: 100M
    nginx.ingress.kubernetes.io/backend-protocol: "HTTPS"
    nginx.ingress.kubernetes.io/session-cookie-samesite: "true"
    nginx.ingress.kubernetes.io/session-cookie-path: "/en-US"
    cert-manager.io/cluster-issuer: selfsigned
  name: splunk-ingress
  namespace: default
spec:
  rules:
  - host: shc.example.com
    http:
      paths:
      - path: /en-US
        pathType: Prefix
        backend:
          serviceName: splunk-shc-search-head-service
          servicePort: 8000
  - host: hec.example.com
    http:
      paths:
      - path: /services/collector
        pathType: Prefix
        backend:
          serviceName: splunk-idc-indexer-service
          servicePort: 8088
  tls:
  - hosts:
    - shc.example.com
    - hec.example.com
    secretName: operator-tls
```


### Configuring Ingress NGINX for Splunk Forwarders with End-to-End TLS


Note: In this example we used port 9997 for non-encrypted communication, and 9998 for encrypted.

Update the default Ingress NGINX configuration to add the ConfigMap and Service ports:

1. Create a configMap to define the port-to-service routing

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

2. Add the two ports into the Service used to configure the Load Balancer

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

##### Documentation tested on Ingress Nginx v1.19.4 and Kubernetes v1.17

## Configuring Ingress Using NGINX Ingress Controller (Nginxinc) 

The Nginx Ingress Controller is an open source version of the F5 product. Please review their documentation below for more details.

[NGINX Ingress Controller Github Repo](https://github.com/nginxinc/kubernetes-ingress)

[NGINX Ingress Controller Docs Home](https://docs.nginx.com/nginx-ingress-controller/overview/)

[NGINX Ingress Controller Annotations Page](https://docs.nginx.com/nginx-ingress-controller/configuration/ingress-resources/advanced-configuration-with-annotations/)


### Install the Nginx Helm Chart
We followed the product's Helm Chart installation guide. It requires a cluster with internet access.
[NGINX Ingress Controller Helm Installation]( https://docs.nginx.com/nginx-ingress-controller/installation/installation-with-helm/)

##### Setup Helm

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

##### Install Ingress
```shell
cd deployments/helm-chart

# Edit and make changes to values.yaml as needed
helm install splunk-nginx nginx-stable/nginx-ingress

#list the helms installed
helm list

NAME            NAMESPACE   REVISION    UPDATED             STATUS      CHART               APP VERSION
splunk-nginx  default     5  2020-10-29 15:03:47.6 EDT    deployed    nginx-ingress-0.7.0 1.9.0

#if needed to update any configs for ingress, update the values.yaml and run upgrade
helm upgrade splunk-nginx  nginx-stable/nginx-ingress
```

##### Configure Ingress services

##### Configure Ingress for Splunk Web and HEC

The following ingress example yaml configures Splunk Web as well as HEC as an operator installed service. HEC is exposed via ssl and Splunk Web is non-ssl.

* TLS to the ingress controller is configured in the `tls:` section of the yaml and `secretName` references a valid [TLS secret](https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets).
* For any backend service that has TLS enabled, a corresponding `nginx.org/ssl-services annotation` is required.

Create Ingress
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
    nginx.org/ssl-services: splunk-standalone-standalone-headless
  name: splunk-ingress
  namespace: default
spec:
  ingressClassName: nginx
  rules:
  - host: splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-standalone-standalone-service
          servicePort: 8000
        path: /en-US
        pathType: Prefix
      - backend:
          serviceName: splunk-standalone-standalone-headless
          servicePort: 8088
        path: /services/collector
        pathType: Prefix
      - backend:
          serviceName: splunk-standalone-standalone-headless
          servicePort: 8089
        path: /.well-known
        pathType: Prefix
  tls:
  - hosts:
    - splunk.example.com
    secretName: operator-tls
status:
  loadBalancer: {}
```

##### Ingress Service for Splunk Forwarders 
Enable the global configuration to setup a listener and transport server

1. Create the GlobalConfiguration:
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
    service: splunk-standalone-standalone-service
    port: 9997
      action:
    pass: s2s-app
```

2. Edit the service to establish a node-port for the port being setup as the listener:
  1. List the service:
  ```yaml
  kubectl get svc
  NAME                                         TYPE           CLUSTER-IP       EXTERNAL-IP                                                               PORT(S)                                                            AGE
  splunk-nginx-nginx-ingress                 LoadBalancer   172.20.195.54    aa725344587a4443b97c614c6c78419c-1675645062.us-east-2.elb.amazonaws.com   80:31452/TCP,443:30402/TCP,30403:30403/TCP                         7d1h
  ```

  2. Edit the service and add the Splunk Forwarder ingress port:
  ```
  kubectl edit service splunk-nginx-nginx-ingress
  ```

Example Service:
```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    meta.helm.sh/release-name: splunk-nginx
    meta.helm.sh/release-namespace: default
  creationTimestamp: "2020-10-23T17:05:08Z"
  finalizers:
  - service.kubernetes.io/load-balancer-cleanup
  labels:
    app.kubernetes.io/instance: splunk-nginx
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/name: splunk-nginx-nginx-ingress
    helm.sh/chart: nginx-ingress-0.7.0
  name: splunk-nginx-nginx-ingress
  namespace: default
  resourceVersion: "3295579"
  selfLink: /api/v1/namespaces/default/services/splunk-nginx-nginx-ingress
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
    app: splunk-nginx-nginx-ingress
  sessionAffinity: None
  type: LoadBalancer
```

##### Documentation tested on Nginx Ingress Controller v1.9.0 and Kubernetes v1.18

## Using Let's Encrypt to manage TLS certificates 

If you are using [cert-manager](https://docs.cert-manager.io/en/latest/getting-started/) with [Let’s Encrypt](https://letsencrypt.org/) to manage your TLS certificates in Kubernetes, this example Ingress object can be used to enable secure (TLS) access to all Splunk components from outside of your Kubernetes cluster:

### Example configuration for NGINX

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
  - host: cluster-manager.splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-example-cluster-manager-service
          servicePort: 8000
  - host: license-manager.splunk.example.com
    http:
      paths:
      - backend:
          serviceName: splunk-example-license-manager-service
          servicePort: 8000
  tls:
  - hosts:
    - splunk.example.com
    - deployer.splunk.example.com
    - cluster-manager.splunk.example.com
    - license-manager.splunk.example.com
    secretName: splunk.example.com-tls
```

The `certmanager.k8s.io/cluster-issuer` annotation is optional, and is used to automatically create and manage certificates for you. You can change it to match your Issuer.

If you are not using cert-manager, you should remove this annotation and update the `tls` section as appropriate. If you are manually importing your certificates into separate secrets for each hostname, you can reference these by using multiple `tls` objects in your Ingress:

```yaml
tls:
  - hosts:
    - splunk.example.com
    secretName: splunk.example.com-tls
  - hosts:
    - deployer.splunk.example.com
    secretName: deployer.splunk.example.com-tls
  - hosts:
    - cluster-manager.splunk.example.com
    secretName: cluster-manager.splunk.example.com-tls
…
```

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
