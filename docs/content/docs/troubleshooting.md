---
title: Troubleshooting
prev: /docs/advanced
weight: 300
toc: false
---


{{% steps %}}

### http: proxy error: tls: failed to verify certificate: x509: certificate

The actual error is a TLS error. The most common cause is that the target has a self-signed certificate.

```yaml
tsdproxy.enable: true
tsdproxy.scheme: https
tsdproxy.tlsvalidate: false

```

### 2024/12/06 15:17:11 http: proxy error: dial tcp 172.18.0.1:8001: i/o timeout

This error is caused by the target not being reachable. It's a network error.

#### Cause: Firewall

Most likely the firewall is blocking the traffic. In case of UFW execute this command:

```bash
sudo ufw allow in from 172.17.0.0/16
```

#### Cause: Failed docker autodetection

Try to disable autodetection and define the port:

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.autodetect: "false"
  tsdproxy.port: 8001
```

{{%/ steps %}}
