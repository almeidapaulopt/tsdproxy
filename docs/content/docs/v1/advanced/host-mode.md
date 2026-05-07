---
title: Service with host network_mode
---

If you want to run a service in `network_mode: host`, TSDProxy tries to detect how
to proxy the container. In case of not autodetection work for your case, you need
to specify a port in the `tsdproxy.container_port` option.

{{% steps %}}

### Add a label to your service

In your service, add the following label, with the port you want to use in the container:

```yaml
labels:
  tsdproxy.container_port: 8080
```

### Restart your service

After you restart your service, you should be able to access it using the port
you specified in the label.

{{% /steps %}}
