---
title: About
type: about
---

TSDProxy is a free, open-source application that automatically creates reverse proxies to virtual addresses in your Tailscale network. It is based on Docker container labels or YAML proxy lists, simplifying traffic redirection to services running inside Docker containers without the need for a separate Tailscale container for each service.

## Why TSDProxy?

- **No extra containers**: Unlike other solutions, TSDProxy doesn't require a separate Tailscale container per service.
- **Label-driven**: Add a few Docker labels to your containers and TSDProxy handles the rest.
- **Automatic HTTPS**: Leverages Tailscale's built-in Let's Encrypt certificate support via MagicDNS.
- **Multiple providers**: Supports both Docker container discovery and YAML proxy list files.
- **Dashboard**: Real-time web dashboard with SSE streaming for monitoring all your proxies.

## License

TSDProxy is licensed under the MIT License. See the [LICENSE](https://github.com/almeidapaulopt/tsdproxy/blob/main/LICENSE) file for details.
