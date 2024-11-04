---
title: Services configuration
prev: /docs/getting-started
next: /docs/advanced
weight: 2

---

To add a service to your TSDProxy instance, you need to add a label to your service cobtainer.

{{% steps %}}

### tsdproxy.enable

Just add the label `tsdproxy.enable` to true and restart you service. The container will be started and TSDProxy will be enabled.

```yaml
labels:
  tsdproxy.enable: "true"
```

TSProxy will use container name as Tailscale server, and will use one exposed port to proxy traffic.

### tsdproxy.name

If you define a name different from the container name, you can define it with the label `tsdproxy.name` and it will be used as the Tailscale server name.

```yaml
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
```

### tsdproxy.container_port

If you want to define a different port than the default one, you can define it with the label `tsdproxy.container_port`.
This is useful if the container has multiple exposed ports or if the container is running in network_mode=host.

```yaml
ports:
  - 8081:8080
  - 8000:8000
labels:
  tsdproxy.enable: "true"
  tsdproxy.name: "myserver"
  tsdproxy.container_port: 8080
```

{{< callout emoji="❓" >}}
Note that the port used in the `tsdproxy.container_port` label is the port used internal in the container and not the exposed port.
{{< /callout >}}

{{% /steps %}}
