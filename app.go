package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/urfave/cli/v2"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type reverseConfig struct {
	Dns, Port, Url, Private string
}

type redirConfig struct {
	Origin, Redirect string
}

const caddyAddTemplate = `{
  "@id": "{{ .Url }}",
  "handle": [
    {
      "handler": "subroute",
      "routes": [
        {{ .Private }}
        {
          "handle": [
            {
              "handler": "headers",
              "response": {
                "set": {
                  "Strict-Transport-Security": [
                    "max-age=31536000;"
                  ]
                }
              }
            },
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

const CaddyRedirTemplate = `{
	"@id": "{{ .Origin }}",
	"handle": [
                {
                  "handler": "subroute",
                  "routes": [
                    {
                      "handle": [
                        {
                          "handler": "static_response",
                          "headers": {
                            "Location": [
                              "{{ .Redirect }}{http.request.uri}"
                            ]
                          },
                          "status_code": 302
                        }
                      ]
                    }
                  ]
                }
              ],
              "match": [
                {
                  "host": [
                    "{{ .Origin }}"
                  ]
                }
              ]}
`

func httpRequest(method string, url string, buffer bytes.Buffer) string {
	client := &http.Client{}

	req, err := http.NewRequest(method, url, &buffer)
	req.Header.Set("Content-Type", "application/json")
	check(err)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Network or Caddy host is unreachable.")
		return "networkError"
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	check(err)

	var prettyJSON bytes.Buffer
	_ = json.Indent(&prettyJSON, body, "", "    ")

	return string(prettyJSON.Bytes())
}

// convert byte to json
func readJsonMap(buffer []byte) map[string]interface{} {
	var result map[string]interface{}
	check(json.Unmarshal(buffer, &result))

	return result
}

// split manual input from flag
func getManualConfig(input string, private bool) reverseConfig {
	config := strings.Split(input, ":")

	return createReverseConfig(config, private)
}

// assembles the reverseConfig struct from provided data (manual or annotation)
func createReverseConfig(input []string, private bool) reverseConfig {
	if len(input) == 0 {
		os.Exit(0)
	} else if len(input) != 3 {
		log.Fatal("Please provide 3 input values separated by ':' - PUBLIC_NAME:INTERN_NAME:INTERN_PORT")
	}

	var reverseConfig reverseConfig

	reverseConfig.Url = input[0]
	reverseConfig.Dns = input[1]
	reverseConfig.Port = input[2]
	if private {
		reverseConfig.Private = `{
                      "handle": [
                        {
                          "abort": true,
                          "handler": "static_response"
                        }
                      ],
                      "match": [
                        {
                          "not": [
                            {
                              "remote_ip": {
                                "ranges": [
                                  "192.168.0.0/16",
                                  "172.16.0.0/12",
                                  "10.0.0.0/8",
                                  "127.0.0.1/8",
                                  "fd00::/8",
                                  "::1"
                                ]
                              }
                            }
                          ]
                        }
                      ]
                    },`
	} else {
		reverseConfig.Private = ""
	}

	return reverseConfig
}

func createProxyTemplate(config reverseConfig) bytes.Buffer {
	t := template.Must(template.New("caddy-reverse").Parse(caddyAddTemplate))
	var tpl bytes.Buffer
	check(t.Execute(&tpl, config))

	return tpl
}

func createRedirTemplate(config redirConfig) bytes.Buffer {
	t := template.Must(template.New("caddy-reverse").Parse(CaddyRedirTemplate))
	var tpl bytes.Buffer
	check(t.Execute(&tpl, config))

	return tpl
}

// checks whether forward flag was used for providing manual config data
func checkFlags(forward string, private bool) reverseConfig {
	return getManualConfig(forward, private)
}

// returns number of current containers route (filtert based on hostname)
func getCaddyRoute(config map[string]interface{}, hostname string) string {
	for id, element := range config["routes"].([]interface{})[:len(config["routes"].([]interface{}))-1] {
		// fuck json in go!
		if element.(map[string]interface{})["match"].([]interface{})[0].(map[string]interface{})["host"].([]interface{})[0] == hostname {
			return strconv.Itoa(id)
		}
	}
	log.Fatal("No route for this host found.")
	return ""
}

func addRoute(reverseConfig reverseConfig, caddyHost string, server string) {
	resp := httpRequest("GET", "http://"+caddyHost+":2019/id/"+reverseConfig.Url, bytes.Buffer{})

	// check whether object with id already exists, if true abort
	if strings.Contains(resp, `"error": "unknown object ID`) {
		tpl := createProxyTemplate(reverseConfig)
		httpRequest("PUT", "http://"+caddyHost+":2019/config/apps/http/servers/"+server+"/routes/0/", tpl)
		log.Println("Added route successfully.")
	} else if !strings.Contains(resp, "networkError") {
		log.Println("Route already exists.")
	}
}

func delRoute(caddyHost string, domain string) {
	log.Println("Deleting route with id " + domain)
	resp := httpRequest("DELETE", "http://"+caddyHost+":2019/id/"+domain, bytes.Buffer{})

	if strings.Contains(resp, `"error":"unknown object ID`) {
		log.Println("No route with matching ID found, searching for route.")
		caddyConf := httpRequest("GET", "http://"+caddyHost+":2019/config/apps/http/servers/srv0/", bytes.Buffer{})
		routeNumber := getCaddyRoute(readJsonMap([]byte(caddyConf)), domain)
		httpRequest("DELETE", "http://"+caddyHost+":2019/config/apps/http/servers/srv0/routes/"+routeNumber, bytes.Buffer{})
	}
}

func addRedir(redirConfig redirConfig, caddyHost string) {
	resp := httpRequest("GET", "http://"+caddyHost+":2019/id/"+redirConfig.Origin, bytes.Buffer{})

	// check whether object with id already exists, if true abort
	if strings.Contains(resp, `"error":"unknown object ID`) {
		httpRequest("PUT", "http://"+caddyHost+":2019/config/apps/http/servers/srv0/routes/0/", createRedirTemplate(redirConfig))
		log.Println("Added route successfully.")
	} else if !strings.Contains(resp, "networkError") {
		log.Println("Route already exists.")
	}
}

func main() {
	var caddyHost, forward, extern, caddyServer string
	var redir redirConfig
	var update int
	var private bool
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
					&cli.IntFlag{
						Name:        "update",
						Aliases:     []string{"up"},
						Usage:       "retries to add the route every n mins in case of unavailable caddy server",
						Value:       0,
						Destination: &update,
					},
					&cli.StringFlag{
						Name:        "server",
						Aliases:     []string{"srv"},
						Value:       "srv0",
						Usage:       "provide the server name used in the caddy configuration",
						EnvVars:     []string{"PODMAN_CADDY_SERVER"},
						DefaultText: "srv0",
						Destination: &caddyServer,
					},
					&cli.BoolFlag{
						Name:        "private",
						Aliases:     []string{"p"},
						Value:       false,
						Usage:       "make this route only reachable via privat ip's",
						Destination: &private,
					},
				},
				Action: func(c *cli.Context) error {
					reverseConfig := checkFlags(forward, private)
					addRoute(reverseConfig, caddyHost, caddyServer)

					// retries route creation every n minutes
					if update != 0 {
						for {
							time.Sleep(time.Duration(update) * time.Minute)
							addRoute(reverseConfig, caddyHost, caddyServer)
						}
					}
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
					&cli.StringFlag{
						Name:        "extern",
						Aliases:     []string{"ex"},
						Usage:       "External domainname used in the route which should be deleted",
						Destination: &extern,
					},
				},
				Action: func(c *cli.Context) error {
					if extern != "" {
						delRoute(caddyHost, extern)
					} else {
						reverseConfig := checkFlags(forward, false)
						delRoute(caddyHost, reverseConfig.Url)
					}

					return nil
				},
			},
			{
				Name:    "ls",
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
					fmt.Println(httpRequest("GET", "http://"+caddyHost+":2019/config/apps/http/servers/", bytes.Buffer{}))
					return nil
				},
			},
			{
				Name:    "redir",
				Aliases: []string{"mv"},
				Usage:   "creates 301 and redirects to provided page",
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
						Name:        "origin",
						Aliases:     []string{"orig"},
						Usage:       "Provide origin which should be redirected (example: test.example.com)",
						Destination: &redir.Origin,
						Required:    true,
					},
					&cli.StringFlag{
						Name:        "redirect",
						Aliases:     []string{"re"},
						Usage:       "Provide redirect location (example: example.com)",
						Destination: &redir.Redirect,
						Required:    true,
					},
				},
				Action: func(c *cli.Context) error {
					addRedir(redir, caddyHost)
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
