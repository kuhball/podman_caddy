package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
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

var dnsServer = "10.89.0.1"

//func writeFile(data string, path string) error {
//	return ioutil.WriteFile(path, []byte(data), 0644)
//}

// use container dns server for service lookup
func lookupDNS(server string, url string) string {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(10000),
			}
			return d.DialContext(ctx, "udp", server+":53")
		},
	}
	ip, _ := r.LookupHost(context.Background(), url)

	return ip[0]
}

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
func readJson(buffer []byte) map[string]interface{} {
	var result map[string]interface{}
	check(json.Unmarshal(buffer, &result))

	return result
}

// open json config file
func openJson(filename string) map[string]interface{} {
	jsonFile, err := os.Open(filename)
	check(err)

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	return readJson(byteValue)
}

// get stdin config for annotations & bundle path
func getStdin() map[string]interface{} {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	check(scanner.Err())

	return readJson(scanner.Bytes())
}

// PUBLIC_NAME:INTERN_NAME:INTERN_PORT
// create reverseConfig object and fill in data from annotations
// TODO: get exposed port from image
func checkAnnotations(annotations []string, configJson map[string]interface{}) reverseConfig {
	var reverseConfig reverseConfig

	reverseConfig.Url = annotations[0]
	if annotations[1] == "" {
		reverseConfig.Dns = configJson["hostname"].(string)
	} else {
		reverseConfig.Dns = annotations[1]
	}
	reverseConfig.Port = annotations[2]

	return reverseConfig
}

// returns number of current containers route (filtert based on hostname)
func getRoute(config map[string]interface{}, hostname string) string {
	for id, element := range config["route"].([]interface{}) {
		if element.(map[string]interface{})["match"].([]interface{})[0].(map[string]interface{})["host"] == hostname {
			return string(id)
		}
	}
	log.Fatal("No route for this host found.")
	return ""
}

// adds route for new container based on the annotation 'reverse-proxy'
// TODO: use namespaces
func addRoute(stdin map[string]interface{}, containerConfig map[string]interface{}) {
	annotations := strings.Split(stdin["annotations"].(map[string]interface{})["reverse-proxy"].(string), ":")
	if len(annotations) == 0 {
		os.Exit(0)
	} else if len(annotations) != 3 {
		log.Fatal("Please provide 3 input values separated by ':' - PUBLIC_NAME:INTERN_NAME:INTERN_PORT - INTERN_NAME is not mandatory")
	}

	reverseConfig := checkAnnotations(annotations, containerConfig)

	t := template.Must(template.New("caddy-reverse").Parse(caddyAddTemplate))
	var tpl bytes.Buffer
	check(t.Execute(&tpl, reverseConfig))

	httpRequest("PUT", "http://"+lookupDNS(dnsServer, "caddy")+":2019/config/apps/http/servers/srv0/routes/0/", tpl)
}

func main() {
	// get stdin config for container
	stdin := getStdin()

	// exits if no annotations are provided
	if stdin["annotations"].(map[string]interface{})["reverse-proxy"] == nil {
		os.Exit(0)
	}

	// read container config file
	containerConfig := openJson(stdin["bundle"].(string) + "/config.json")

	// check whether route should be added or deleted
	if os.Args[1] == "add" {
		addRoute(stdin, containerConfig)
	} else if os.Args[1] == "delete" {
		// get current caddy routes
		caddyConf := httpRequest("GET", "http://"+lookupDNS(dnsServer, "caddy")+":2019/config/apps/http/servers/srv0", bytes.Buffer{})
		routeNumber := getRoute(readJson([]byte(caddyConf)), containerConfig["hostname"].(string))
		// delete container route
		httpRequest("DELETE", "http://"+lookupDNS(dnsServer, "caddy")+":2019/config/apps/http/servers/srv0/routes/"+routeNumber, bytes.Buffer{})
	} else {
		log.Fatal("please use argument 'add' or 'delete'")
	}

	return
}
