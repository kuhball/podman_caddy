package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
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

//var dnsServer = "10.89.0.1"
//
//// use container dns server for service lookup
//func lookupDNS(server string, url string) string {
//	r := &net.Resolver{
//		PreferGo: true,
//		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
//			d := net.Dialer{
//				Timeout: time.Millisecond * time.Duration(10000),
//			}
//			return d.DialContext(ctx, "udp", server+":53")
//		},
//	}
//	ip, _ := r.LookupHost(context.Background(), url)
//
//	return ip[0]
//}

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

// open json config file
func openJson(filename string) map[string]interface{} {
	jsonFile, err := os.Open(filename)
	check(err)

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	return readJsonMap(byteValue)
}

// get stdin config for annotations & bundle path
func getStdin() map[string]interface{} {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	check(scanner.Err())

	return readJsonMap(scanner.Bytes())
}

// PUBLIC_NAME:INTERN_NAME:INTERN_PORT
// create reverseConfig object and fill in data from annotations
// all needed for poststop hook, no config file available for getting hostname
// TODO: get exposed port from image
func getAnnotations(stdin map[string]interface{}, configJson map[string]interface{}, all bool) reverseConfig {
	annotations := strings.Split(stdin["annotations"].(map[string]interface{})["de.gaengeviertel.reverse-proxy"].(string), ":")
	if len(annotations) == 0 {
		os.Exit(0)
	} else if len(annotations) != 3 {
		log.Fatal("Please provide 3 input values separated by ':' - PUBLIC_NAME:INTERN_NAME:INTERN_PORT - INTERN_NAME is not mandatory")
	}

	var reverseConfig reverseConfig

	reverseConfig.Url = annotations[0]
	if all {
		if annotations[1] == "" {
			reverseConfig.Dns = configJson["hostname"].(string)
		} else {
			reverseConfig.Dns = annotations[1]
		}
	}

	reverseConfig.Port = annotations[2]

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
// TODO: if port 80 is used, exclude from https cert
func addRoute(config reverseConfig) bytes.Buffer {
	t := template.Must(template.New("caddy-reverse").Parse(caddyAddTemplate))
	var tpl bytes.Buffer
	check(t.Execute(&tpl, config))

	return tpl
}

func main() {
	mode := flag.String("mode", "add", "adding or deleting a route in caddy")
	caddyHost := flag.String("name", "caddy", "hostname or ip of the caddy container")
	useConfig := flag.Bool("use-config", false, "if true config.json will be used for getting internal hostname")

	flag.Parse()

	// get stdin config for container
	stdin := getStdin()

	// exits if no annotations are provided
	if stdin["annotations"].(map[string]interface{})["de.gaengeviertel.reverse-proxy"] == nil {
		os.Exit(0)
	}

	// check whether route should be added or deleted
	if *mode == "add" {
		var reverseConfig reverseConfig
		if *useConfig {
			// read container config file
			containerConfig := openJson(stdin["bundle"].(string) + "/config.json")
			reverseConfig = getAnnotations(stdin, containerConfig, true)
		} else {
			// read container config file
			reverseConfig = getAnnotations(stdin, nil, false)
		}
		tpl := addRoute(reverseConfig)

		httpRequest("PUT", "http://"+*caddyHost+":2019/config/apps/http/servers/srv0/routes/0/", tpl)
	} else if *mode == "delete" {
		reverseConfig := getAnnotations(stdin, nil, false)
		// get current caddy routes
		caddyConf := httpRequest("GET", "http://"+*caddyHost+":2019/config/apps/http/servers/srv0/", bytes.Buffer{})
		routeNumber := getCaddyRoute(readJsonMap([]byte(caddyConf)), reverseConfig.Url)
		// delete container route
		httpRequest("DELETE", "http://"+*caddyHost+":2019/config/apps/http/servers/srv0/routes/"+routeNumber, bytes.Buffer{})
	} else {
		log.Fatal("please use argument 'add' or 'delete'")
	}

	return
}
