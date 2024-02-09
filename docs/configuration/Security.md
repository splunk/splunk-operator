---
title: Secure Splunk deployments
nav_order: 27
#nav_exclude: true
---

# Secure Splunk deployments in Kubernetes

Before creating new deployments in Kubernetes, it is important you consider the security aspects of your environment. This document describes how to secure your deployment using the Operator and Splunk Enterprise configurations, and provides examples for some common scenarios.

Splunk Enterprise provides a range of security frameworks that are also available in your Kubernetes deployment.  Take the time to familiarize yourself with these frameworks in the [Securing the Splunk Platform](https://docs.splunk.com/Documentation/Splunk/latest/Security/WhatyoucansecurewithSplunk) manual.

Your deployment might require communication with Splunk Enterprise instances running outside of Kubernetes. For example, forwarders outside of the Kubernetes cluster sending data into an Indexer Cluster running in Kubernetes. This can be done using a [Ingress Controllers](https://kubernetes.io/docs/concepts/services-networking/ingress/) such as Istio, Nginx, among others. In this documentation we use Istio for our examples.

The procedures to secure your deployment are the same regardless of your choice for Ingress Controller.  See the table below for the ingress communications supported by the Operator:

**Supported communications from outside of the cluster**

|Outside Kubernetes   | SSL/TLS Configuration | Supported |
|---|---|---|
|Forwarders   | Gateway Termination | YES   |
|Forwarders   | End-to-End Termination | YES   |
|Splunk Web   | End-to-End Termination | YES  |
|REST API   | End-to-End Termination | YES  |
|Splunk Web   | Gateway Termination | NO  |
|REST API   | Gateway Termination | NO  |

For more information about how to setup Ingress Controllers using the Operator visit [Ingress Documentation](https://github.com/splunk/splunk-operator/blob/develop/docs/Ingress.md).

## Prerequisites

##### Learn about securing communication channels

Use SSL/TLS certificates to secure the communication between Splunk platform instances. See [Secure Splunk with SSL](https://docs.splunk.com/Documentation/Splunk/latest/Security/AboutsecuringyourSplunkconfigurationwithSSL).


##### Prepare your Certificates

For information on how to create your own certificates or how to sign third-party certificates, see: [How to Sign Certificates](https://docs.splunk.com/Documentation/Splunk/latest/Security/Howtoself-signcertificates)

## Securing Splunk Web using Certificates

In this example, the certificates and configuration files are placed into one app and deployed to a standalone Splunk Enterprise instance, and the Kubernetes Ingress controller is configured to allow inbound communications on port 8000 (Splunk Web.)

* Configure web.conf to enable encryption on Splunk Web. Create your configuration file using the step here: [Secure Splunk Web with TLS](https://docs.splunk.com/Documentation/Splunk/latest/Security/SecureSplunkWebusingasignedcertificate).

* Create an app with both certificates and the pre-configured web.conf file. In the example below, the app named "myapp" includes the certificates and the minimum configuration files required to enable the app and SSL/TLS communications.

```
myapp
├── default
│   ├── app.conf
│   └── web.conf
└── mycerts
    ├── mySplunkCertificate.pem
    └── mySplunkPrivateKey.key
```

Sample app.conf
```
[install]
is_configured = 0

[ui]
is_visible = 1
label = MyApp

[launcher]
author = Splunk
description = My Splunk App
version = 1.0
```

Sample web.conf

```
[settings]
enableSplunkWebSSL = true
privKeyPath = $SPLUNK_HOME/etc/apps/myapp/mycerts/mySplunkPrivateKey.key
serverCert =  $SPLUNK_HOME/etc/apps/myapp/mycerts/mySplunkCertificate.pem
```

* Deploy the app to the Splunk Enterprise instance. See [Install Apps using Splunk Operator](https://github.com/splunk/splunk-operator/blob/develop/docs/Examples.md#installing-splunk-apps). 

* Create the Ingress configuration to allow access to port 8000. This configuration creates a gateway and virtual service for passing the traffic through to the Splunk Enterprise instance.

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: splunk-web
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 8000
      name: ui
      protocol: TCP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
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
    - port: 8000
    route:
    - destination:
        host: splunk-standalone-standalone-service
        port:
          number: 8000
```

* Apply the patch to allow external communication to port 8000.

```bash
kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-web","port":8000,"protocol":"TCP"}]}}'
```

* Verify Splunk Web is now using SSL/TLS by prepending "https://" to the URL you use to access Splunk Web.


## Securing Rest APIs 

In this example, the certificates and configuration files are placed into one app and deployed to a standalone Splunk Enterprise instance, and the Kubernetes Ingress controller is configured to allow inbound communications on port 8089 (Splunk Management.) The method to secure communications for REST API access is similar to the Splunk Web example above.

* Configure server.conf to enable encryption using your certificates. Create the configuration file using the step here: [Secure Clients with SSL](https://docs.splunk.com/Documentation/Splunk/latest/Security/Securingyourdeploymentserverandclients). 

* Create an app with both certificates and the pre-configured server.conf file. In the example below, the app named "myapp" includes the certificates and the minimum configuration files required to enable the app and SSL/TLS communications.

```
myapp
├── default
│   ├── app.conf
│   └── server.conf
└── mycerts
    ├── mySplunkCertificate.pem
    └── mySplunkPrivateKey.key
```

Sample app.conf
```
[install]
is_configured = 0

[ui]
is_visible = 1
label = MyCertificatesApp

[launcher]
author = Splunk
description = My Splunk App for Custom Configuration
version = 1.0
```

Sample server.conf
```
[sslConfig]
sslVersions = *,-ssl2
serverCert = $SPLUNK_HOME/etc/apps/myapp/mycerts/mySplunkAWSCertificate.pem
sslRootCAPath = $SPLUNK_HOME/etc/apps/myapp/mycerts/myCACertificate.pem
sslPassword = <password>
```

* Create the Ingress configuration to allow access to port 8089. This configuration creates a gateway and virtual service for passing the traffic through to the Splunk Enterprise instance.

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: splunk-api
spec:
  selector:
    istio: ingressgateway # use istio default ingress gateway
  servers:
  - port:
      number: 8089
      name: mgmt
      protocol: TCP
    hosts:
    - "*"
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: splunk-api
spec:
  hosts:
  - "splunk.example.com"
  gateways:
  - "splunk-api"
  tcp:
  - match:
    - port: 8089
    route:
    - destination:
        port:
          number: 8089
        host: splunk-standalone-standalone-service
```

* Apply the patch to allow external communication to port 8089.

```bash
kubectl patch -n istio-system service istio-ingressgateway --patch '{"spec":{"ports":[{"name":"splunk-api","port":8089,"protocol":"TCP"}]}}'
```

* Verify the management port is using your certificates. Use the curl command and the path to the client side certificates to authenticate:

```bash
curl --cacert <PathTomyCACertificate.pem> -s -u admin -L "https://<domain:8089>/<api>"
```

Note: You can avoid entering your password in the curl command by using a sessionKey. To learn more about the different forms of authentication, see: [REST Authentication methods](https://docs.splunk.com/Documentation/Splunk/latest/RESTUM/RESTusing#Authentication_and_authorization)

Learn more about APIs available here: [REST Manual](https://docs.splunk.com/Documentation/SplunkCloud/latest/RESTREF/RESTlist) 

## Securing Forwarders

For examples on configuring the Ingress controller to accept data from Forwarders, and securing the data in Kubernetes, see: [Secure Forwarding](https://github.com/splunk/splunk-operator/blob/develop/docs/Ingress.md)


## Password Management 

In Kubernetes, sensitive information such as passwords, OAuth tokens, and ssh keys should be stored using the Secrets objects. Learn how to manage your passwords for Splunk Enterprise deployments in: [Password Management](https://github.com/splunk/splunk-operator/blob/develop/docs/PasswordManagement.md)
