// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

var apiCommand *Command

// GetAPIHandler returns a catchall command handler
func GetAPIHandler() *Command {
	return apiCommand
}

func init() {
	apiCommand = &Command{
		Name: "api",
		Help: "Runs a provided API",
		Handle: func(r *Request) error {
			if len(r.Args) == 0 {
				return errors.New("please provide an API to execute")
			}

			apiName := strings.ToLower(r.Args[0])
			apiArgs := r.Args[1:]
			if r.Config.GetCache()[apiName] == nil && len(r.Args) > 1 {
				apiName = strings.ToLower(strings.Join(r.Args[:2], ""))
				apiArgs = r.Args[2:]
			}

			for _, arg := range r.Args {
				if arg == "-h" {
					r.Args[0] = apiName
					return helpCommand.Handle(r)
				}
			}

			api := r.Config.GetCache()[apiName]
			if api == nil {
				return errors.New("unknown command or API requested")
			}

			var missingArgs []string
			for _, required := range api.RequiredArgs {
				required = strings.ReplaceAll(required, "=", "")
				provided := false
				for _, arg := range apiArgs {
					if strings.Contains(arg, "=") && strings.HasPrefix(arg, required) {
						provided = true
					}
				}
				if !provided {
					missingArgs = append(missingArgs, strings.Replace(required, "=", "", -1))
				}
			}

			if len(missingArgs) > 0 {
				fmt.Println("💩 Missing required parameters: ", strings.Join(missingArgs, ", "))
				return nil
			}

			response, err := NewAPIRequest(r, api.Name, apiArgs, api.Async)
			if err != nil {
				if strings.HasSuffix(err.Error(), "context canceled") {
					return nil
				} else if response != nil {
					printResult(r.Config.Core.Output, response, nil, nil)
				}
				return err
			}

			var filterKeys []string
			for _, arg := range apiArgs {
				if strings.HasPrefix(arg, "filter=") {
					for _, filterKey := range strings.Split(strings.Split(arg, "=")[1], ",") {
						if len(strings.TrimSpace(filterKey)) > 0 {
							filterKeys = append(filterKeys, strings.TrimSpace(filterKey))
						}
					}
				}
			}

			var excludeKeys []string
			for _, arg := range apiArgs {
				if strings.HasPrefix(arg, "exclude=") {
					for _, excludeKey := range strings.Split(strings.Split(arg, "=")[1], ",") {
						if len(strings.TrimSpace(excludeKey)) > 0 {
							excludeKeys = append(excludeKeys, strings.TrimSpace(excludeKey))
						}
					}
				}
			}

			if len(response) > 0 {
				printResult(r.Config.Core.Output, response, filterKeys, excludeKeys)
				if r.Config.HasShell {
					apiName := strings.ToLower(api.Name)
					if apiName == "getuploadparamsforiso" ||
						apiName == "getuploadparamsforvolume" ||
						apiName == "getuploadparamsfotemplate" {
						promptForFileUpload(r, apiName, response)
					}
				}
			}
			return nil
		},
	}
}

func promptForFileUpload(r *Request, api string, response map[string]interface{}) {
	fmt.Print("Enter path of the file(s) to upload (comma-separated): ")
	var filePaths string
	fmt.Scanln(&filePaths)
	filePathsList := strings.FieldsFunc(filePaths, func(r rune) bool { return r == ',' })

	var missingFiles []string
	var validFiles []string
	for _, filePath := range filePathsList {
		filePath = strings.TrimSpace(filePath)
		if filePath == "" {
			continue
		}
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			missingFiles = append(missingFiles, filePath)
		} else {
			validFiles = append(validFiles, filePath)
		}
	}
	if len(missingFiles) > 0 {
		fmt.Println("File(s) do not exist or are not accessible:", strings.Join(missingFiles, ", "))
		return
	}
	if len(validFiles) == 0 {
		fmt.Println("No valid files to upload.")
		return
	}
	paramsRaw, ok := response["getuploadparams"]
	if !ok || reflect.TypeOf(paramsRaw).Kind() != reflect.Map {
		fmt.Println("Invalid response format for getuploadparams.")
		return
	}
	params := paramsRaw.(map[string]interface{})
	requiredKeys := []string{"postURL", "metadata", "signature", "expires"}
	for _, key := range requiredKeys {
		if _, ok := params[key]; !ok {
			fmt.Printf("Missing required key '%s' in getuploadparams response.\n", key)
			return
		}
	}

	postURL, _ := params["postURL"].(string)
	signature, _ := params["signature"].(string)
	expires, _ := params["expires"].(string)
	metadata, _ := params["metadata"].(string)

	fmt.Println("Uploading files for", api, ":", validFiles)
	spinner := r.Config.StartSpinner("uploading files, please wait...")
	defer r.Config.StopSpinner(spinner)

	for _, filePath := range validFiles {
		if err := uploadFile(postURL, filePath, signature, expires, metadata); err != nil {
			fmt.Println("Error uploading", filePath, ":", err)
		}
	}
}

// uploadFile uploads a single file to the given postURL with the required headers.
func uploadFile(postURL, filePath, signature, expires, metadata string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	writer.Close()

	req, err := http.NewRequest("POST", postURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("x-signature", signature)
	req.Header.Set("x-expires", expires)
	req.Header.Set("x-metadata", metadata)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed: %s", string(respBody))
	}
	fmt.Println("Upload successful for:", filePath)
	return nil
}
