---
title: Dashboard
prev: /docs/v2/advanced
---

{{% steps %}}

### Via Docker labels

```yaml
labels:
  - tsdproxy.enable=true
  - tsdproxy.name=dash
  - tsdproxy.port.1=443/https:8080/http
```

### Via Lists provider

```yaml
dash:
  ports:
    443/https:
      targets:
        - http://127.0.0.1:8080
```

### Test

```bash
curl https://dash.FUNNY-NAME.ts.net
```

{{% /steps %}}
