# podman caddy
This tool creates reverse-proxy entries in [caddy](https://caddyserver.com/). It's running within an seperate container in every pod for announcing the needed caddy route.  

## install

### tool

Within the provided Dockerfile a build stage is used for building the image. Afterwords it runs in a scratch container to stay as small as possible. 

Following arguments can be provided:

```
# podman run --rm podman_caddy --help
  NAME:
     podman_caddy - create caddy routes from a podman context
  
  USAGE:
     podman_caddy [global options] command [command options] [arguments...]
  
  COMMANDS:
     add, a      add a route to caddy
     remove, rm  delete a route from caddy
     ls, ls      displays current caddy config
     redir, mv   creates 301 and redirects to provided page
     help, h     Shows a list of commands or help for one command
  
  GLOBAL OPTIONS:
     --help, -h  show help (default: false)
```

For every command there are several options like:

```
# podman run --rm podman_caddy add --help
NAME:
   podman_caddy add - add a route to caddy

USAGE:
   podman_caddy add [command options] [arguments...]

OPTIONS:
   --caddyHost value, --ca value  Provide the caddy hostname or IP manually (default: caddy) [$PODMAN_CADDY_HOST]
   --forward value, --fw value    Provide route details in the format PUBLIC_NAME:INTERN_NAME:INTERN_PORT [$PODMAN_CADDY_FORWARD]
   --update value, --up value     retries to add the route every n mins in case of unavailable caddy server (default: 0)
   --server value, --srv value    provide the server name used in the caddy configuration (default: srv0) [$PODMAN_CADDY_SERVER]
   --help, -h                     show help (default: false)
```

### tool in a container

For building the container use the following command:

```bash
podman build --rm -t podman_caddy:latest .
```

### caddy container

```bash
podman run --rm -it -p 80:80 -p 443:443 -v caddy_config:/config --name caddy --hostname caddy docker.io/caddy/caddy caddy run --config /config/config.json
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
podman run --rm podman_caddy add --fw test.local:dieter:80
podman run -it --rm --name dieter --hostname dieter --network dns_test alpine_nginx
```
