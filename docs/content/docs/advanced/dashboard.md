---
title: Dashboard
prev: /docs/advanced
# next: /docs/advanced/host-mode
---

{{< callout type="warning" >}}
Dashboard is still in very early stages of development.
{{< /callout >}}

{{% steps %}}

### TSDProxy docker compose

Update docker-compose.yml with the following:

```yaml docker-compose.yml
    labels:
      - tsdproxy.enable=true
      - tsdproxy.name=dash
```

### Restart TSDProxy

```bash
docker compose restart
```

### Test Dashboard access

```bash
curl https://dash.FUNNY-NAME.ts.net
```

{{< callout type="info" >}}
Note that you need to replace `FUNNY-NAME` with the name of your network.
{{< /callout >}}

{{% /steps %}}
