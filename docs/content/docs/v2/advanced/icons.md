---
title: Dashboard icons
---

TSDProxy supports three comprehensive icon libraries:

1. **Material Design Icons** [pictogrammers.com/library/mdi](https://pictogrammers.com/library/mdi/),
offering a vast collection of intuitive and versatile icons. Use "mdi/" as the prefix.
2. **Simple Icons** [simpleicons.org](https://simpleicons.org), which includes
icons for popular brands and services. Use "si/" as prefix.
3. **Selfh.st Icons** [selfh.st/icons](https://selfh.st/icons/),
collection of icons and logos for self-hosted dashboards. Use "sh/" as prefix.

>[!NOTE]
> Only SVG icons are available.

## How it works

1. Select the icon in icon libraries websites.
2. Add the definition to your proxy "tsdproxy.dash.icon" in [docker provider](/docs/v2/providers/docker/#tsdproxydashicon)
or "icon" in dashboard section for [Proxy List](/docs/v2/providers/lists/#proxy-list-file-options)
3. Set the icon definition to "library/icon"
(don't add extension, TSDProxy will add .svg)

## Examples:

"si/tailscale" [simpleicons.org/?q=tailscale](https://simpleicons.org/?q=tailscale).

"mdi/music-box" [pictogrammers.com/library/mdi/icon/music-box](https://pictogrammers.com/library/mdi/icon/music-box/).

"sh/adguard-home" [selfh.st/icon/](https://selfh.st/icons/). With the mouse
hover on the "svg" icon, you can see the name of the icon.
