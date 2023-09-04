# Dynamic incoming port - Docker mod for deluge

Adds the ability for deluge to use a dynamic incoming port via NAT-PMP. For
example, with esoteric routers like ProtonVPN.

In deluge docker arguments, set a couple environment variables:

```sh
DOCKER_MODS=ghcr.io/JeffreyFalgout/deluge-dynamic-incoming-port:latest
NATPMP_GATEWAY=10.2.0.1
```

# Mod creation instructions

* After all init scripts and services are created, run `find ./  -path "./.git" -prune -o \( -name "run" -o -name "finish" -o -name "check" \) -not -perm -u=x,g=x,o=x -print -exec chmod +x {} +` to fix permissions.
* Ask the team to create a new branch named `<baseimagename>-<modname>`. Baseimage should be the name of the image the mod will be applied to. The new branch will be based on the `template` branch.
* Submit PR against the branch created by the team.
