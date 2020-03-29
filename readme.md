# podman caddy
This tool creates reverse-proxy entries in [caddy](https://caddyserver.com/). For calling the tool automatically when creating a new container in podman OCI hooks are used.

## install

### hooks 

Adapt the used arguments to your environment. 

#### binary

You need 2 hooks. These are placed in `/etc/containers/oci/hooks.d/` or `/usr/share/containers/oci/hooks.d/`

```
{
  "version": "1.0.0",
  "hook": {
    "path": "PathToBinary",
    "args": ["podman_caddy", "-mode=add"]
  },
  "when": {
    "annotations": {
	"de.gaengeviertel.reverse-proxy":".*:.*:.*"
   }
  },
  "stages": ["poststart"]
}
```

```
{
  "version": "1.0.0",
  "hook": {
    "path": "PathToBinary",
    "args": ["podman_caddy", "-mode=delete"]
  },
  "when": {
    "annotations": {
	"de.gaengeviertel.reverse-proxy":".*:.*:.*"
   }
  },
  "stages": ["poststop"]
}
```

#### container

```
{
  "version": "1.0.0",
  "hook": {
    "path": "/bin/podman",
    "args": ["podman", "run", "--rm", "-i", "-a", "stdin","--network", "dns_test", "podman_caddy", "-mode=add"]
  },
  "when": {
    "annotations": {
	"de.gaengeviertel.reverse-proxy":".*:.*:.*"
   }
  },
  "stages": ["poststart"]
}
```

```
{
  "version": "1.0.0",
  "hook": {
    "path": "/usr/local/bin/wrapper.sh",
    "args": ["wrapper.sh", "podman", "run", "--rm", "-i", "-a", "stdin","--network", "dns_test", "podman_caddy", "-mode=delete"]
  },
  "when": {
    "annotations": {
	"de.gaengeviertel.reverse-proxy":".*:.*:.*"
   }
  },
  "stages": ["poststop"]
}
```



### tool

Within the provided Dockerfile a build stage is used for building the image. Afterwords it runs in a scratch container to stay as small as possible. 

Following arguments can be provided:

- `mode`: `add` / `delete` , default: `add`
- `name`: Hostname or IP Address of the caddy container, default:`caddy`
- `use-config`: bool, if true the config.json from the bundle path is used for optaining the hostname of the created container (only possible if the tool is running on the host system), default: `false`



### tool in a container

For building the container use the following command:

```bash
podman build --rm -t podman_caddy:latest .
```

Due to a missing PATH in the poststop hook you need a wrapper around the command:

```sh
#!/bin/sh

export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

exec "$@"
```

### caddy container

```bash
podman run --rm -it -p 80:80 -p 443:443 -v caddy_config:/config --name caddy --hostname caddy --network dns_test docker.io/caddy/caddy caddy run --config /config/config.json
```

caddy config file:

```json
{
  "admin": {
    "listen": "0.0.0.0:2019",
    "config": {
      "persist": false
    }
  },
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "listen": [
            ":80",
            ":443"
          ],
          "routes": [{}]
        }
      }
    }
  }
}
```

The config file is needed for altering the admin api to listen to requests outside of localhost and make the config inpersistent. Otherwise the container will keep all the routes he had when stopping. 

It's important to make sure the first container started in the environment is the caddy container. This can be done by using systemd. 

## test

```bash
podman run -it --rm --name dieter --hostname dieter --network dns_test --annotation de.gaengeviertel.reverse-proxy=dieter:dieter:80 log-level debug  alpine_nginx
```

