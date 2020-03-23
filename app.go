package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/varlink/go/varlink"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"text/template"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type reverseConfig struct {
	Dns, Port, Url  string
}


const caddyTemplate = `{
  "apps": {
    "http": {
      "servers": {
        "srv0": {
          "routes": [
            {
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
            }
          ]
        }
      }
    }
  }
}`


func writeFile(data string, path string) error {
	return ioutil.WriteFile(path, []byte(data), 0644)
}

func openConfig(filename string) map[string]interface{} {
	jsonFile, err := os.Open(filename)

	check(err)

	fmt.Println("Successfully opened file.")

	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	var result map[string]interface{}
	check(json.Unmarshal(byteValue, &result))

	return result
}

func getStdin() map[string]interface{} {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	check(scanner.Err())

	var result map[string]interface{}
	check(json.Unmarshal(scanner.Bytes(), &result))

	return result
}

func getIP(hostname string, path string) (string, error) {
	files, err := ioutil.ReadDir(path)
	check(err)

	for _, file := range files {
		if !file.IsDir() {
			dat, err := ioutil.ReadFile(path + file.Name())
			check(err)

			fmt.Println(string(dat))

			if string(dat[0:12]) == hostname || string(dat[:64]) == hostname {
				return file.Name(), nil
			}
		}
	}

	return "", errors.New("no IP Address found")
}

// PUBLIC_NAME:INTERN_NAME:INTERN_PORT
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

var c *varlink.Connection
var err error

func main() {
	stdin := getStdin()

	annotations := strings.Split(stdin["annotations"].(map[string]interface{})["reverse-proxy"].(string), ":")
	fmt.Println(len(annotations))
	if len(annotations) == 0 {
		os.Exit(1)
	} else if len(annotations) != 3 {
		log.Fatal("Please provide 3 input values separated by ':' - PUBLIC_NAME:INTERN_NAME:INTERN_PORT - INTERN_NAME is not necessary")
	}

	configJson := openConfig(stdin["bundle"].(string) + "/config.json")

	reverseConfig := checkAnnotations(annotations, configJson)

	t := template.Must(template.New("caddy-reverse").Parse(caddyTemplate))
	var tpl bytes.Buffer
	check(t.Execute(&tpl,reverseConfig))

	resp, err := http.Post("","application/json", &tpl)
	check(err)

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	check(err)

	writeFile("/root/response_body", string(body))
}
