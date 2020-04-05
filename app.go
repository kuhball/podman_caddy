package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"gopkg.in/urfave/cli.v2"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type reverseConfig struct {
	Dns, Port, Url string
}

const caddyAddTemplate = `{
  "handle": [
    {
      "handler": "subroute",
      "routes": [
        {
          "handle": [
            {
              "handler": "reverse_proxy",
              "headers": {
				"request": {
				  "set": {
					"X-Forwarded-Proto": [
					  "{http.request.scheme}"
					],
					"X-Real-Ip": [
					  "{http.request.remote.host}"
					],
					"X-Forwarded-For": [
					  "{http.request.remote.host}"
					],
					"Forwarded": [
					  "for={http.request.remote.host};host={http.request.hostport};proto={http.request.scheme}"
					]
				  }
				}
			  },
              "upstreams": [
                {
                  "dial": "{{ .Dns }}:{{ .Port }}"
                }
              ]
            }
          ]
        }
      ]
    }
  ],
  "match": [
    {
      "host": [
        "{{ .Url }}"
      ]
    }
  ]
}`

func httpRequest(method string, url string, buffer bytes.Buffer) string {
	client := &http.Client{}

	req, err := http.NewRequest(method, url, &buffer)
	check(err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	check(err)

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	check(err)

	return string(body)
}

// convert byte to json
func readJsonMap(buffer []byte) map[string]interface{} {
	var result map[string]interface{}
	check(json.Unmarshal(buffer, &result))

	return result
}

// get stdin config for annotations & bundle path
func getStdin() map[string]interface{} {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	check(scanner.Err())

	return readJsonMap(scanner.Bytes())
}

// get annotation from stdin and split it
func getStdinConfig(stdin map[string]interface{}) reverseConfig {
	annotations := strings.Split(stdin["annotations"].(map[string]interface{})["de.gaengeviertel.reverse-proxy"].(string), ":")

	return createReverseConfig(annotations)
}

// split manual input from flag
func getManualConfig(input string) reverseConfig {
	config := strings.Split(input, ":")

	return createReverseConfig(config)
}

// assembles the reverseConfig struct from provided data (manual or annotation)
func createReverseConfig(input []string) reverseConfig {
	if len(input) == 0 {
		os.Exit(0)
	} else if len(input) != 3 {
		log.Fatal("Please provide 3 input values separated by ':' - PUBLIC_NAME:INTERN_NAME:INTERN_PORT")
	}

	var reverseConfig reverseConfig

	reverseConfig.Url = input[0]
	reverseConfig.Dns = input[1]
	reverseConfig.Port = input[2]

	return reverseConfig
}

// returns number of current containers route (filtert based on hostname)
func getCaddyRoute(config map[string]interface{}, hostname string) string {
	for id, element := range config["routes"].([]interface{}) {
		// fuck json in go!
		if element.(map[string]interface{})["match"].([]interface{})[0].(map[string]interface{})["host"].([]interface{})[0] == hostname {
			return strconv.Itoa(id)
		}
	}
	log.Fatal("No route for this host found.")
	return ""
}

// adds route for new container based on the annotation 'reverse-proxy'
func addRoute(config reverseConfig) bytes.Buffer {
	t := template.Must(template.New("caddy-reverse").Parse(caddyAddTemplate))
	var tpl bytes.Buffer
	check(t.Execute(&tpl, config))

	return tpl
}

// checks whether forward flag was used for providing manual config data
func checkFlags(forward string) reverseConfig {
	if forward == "" {
		return getStdinConfig(getStdin())
	} else {
		return getManualConfig(forward)
	}

}

func main() {
	var caddyHost, forward string
	app := &cli.App{
		Name:  "podman_caddy",
		Usage: "create caddy routes from a podman context",
		Commands: []*cli.Command{
			{
				Name:    "add",
				Aliases: []string{"a"},
				Usage:   "add a route to caddy",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "caddyHost",
						Aliases:     []string{"ca"},
						Value:       "caddy",
						Usage:       "Provide the caddy hostname or IP manually",
						EnvVars:     []string{"PODMAN_CADDY_HOST"},
						DefaultText: "caddy",
						Destination: &caddyHost,
					},
					&cli.StringFlag{
						Name:        "forward",
						Aliases:     []string{"fw"},
						Usage:       "Provide route details in the format PUBLIC_NAME:INTERN_NAME:INTERN_PORT",
						EnvVars:     []string{"PODMAN_CADDY_FORWARD"},
						Destination: &forward,
					},
				},
				Action: func(c *cli.Context) error {
					reverseConfig := checkFlags(forward)
					tpl := addRoute(reverseConfig)

					httpRequest("PUT", "http://"+caddyHost+":2019/config/apps/http/servers/srv0/routes/0/", tpl)
					return nil
				},
			},
			{
				Name:    "remove",
				Aliases: []string{"rm"},
				Usage:   "delete a route from caddy",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "caddyHost",
						Aliases:     []string{"ca"},
						Value:       "caddy",
						Usage:       "Provide the caddy hostname or IP manually",
						EnvVars:     []string{"PODMAN_CADDY_HOST"},
						DefaultText: "caddy",
						Destination: &caddyHost,
					},
					&cli.StringFlag{
						Name:        "forward",
						Aliases:     []string{"fw"},
						Usage:       "Provide route details in the format PUBLIC_NAME:INTERN_NAME:INTERN_PORT",
						EnvVars:     []string{"PODMAN_CADDY_FORWARD"},
						Destination: &forward,
					},
				},
				Action: func(c *cli.Context) error {
					reverseConfig := checkFlags(forward)

					// get current caddy routes
					caddyConf := httpRequest("GET", "http://"+caddyHost+":2019/config/apps/http/servers/srv0/", bytes.Buffer{})
					routeNumber := getCaddyRoute(readJsonMap([]byte(caddyConf)), reverseConfig.Url)
					// delete container route
					httpRequest("DELETE", "http://"+caddyHost+":2019/config/apps/http/servers/srv0/routes/"+routeNumber, bytes.Buffer{})
					return nil
				},
			},
			{
				Name:    "ls",
				Aliases: []string{"ls"},
				Usage:   "displays current caddy config",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:        "caddyHost",
						Aliases:     []string{"ca"},
						Value:       "caddy",
						Usage:       "Provide the caddy hostname or IP manually",
						EnvVars:     []string{"PODMAN_CADDY_HOST"},
						DefaultText: "caddy",
						Destination: &caddyHost,
					},
				},
				Action: func(c *cli.Context) error {
					// get current caddy routes
					fmt.Println(httpRequest("GET", "http://"+caddyHost+":2019/config/apps/http/servers/srv0/", bytes.Buffer{}))
					return nil
				},
			},
		},
	}
	app.EnableBashCompletion = true

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
