---
title: Headscale
draft: true
---

In case you want to use the Headscale service, please read the following:

{{% steps %}}

### In your TSDProxy docker-compose.yaml

Add the following to the `environment` section:

```yaml docker-compose.yml
   environment:
      - TSDPROXY_CONTROLURL="url of you headscale server"
```

### Restart TSDProxy

```bash
docker compose restart
```

{{% /steps %}}
