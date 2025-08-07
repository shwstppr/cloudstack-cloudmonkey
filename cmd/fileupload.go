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
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/briandowns/spinner"
)

const (
	uploadingMessage = "Uploading files, please wait..."
)

// PromptAndUploadFileIfNeeded prompts the user to provide file paths for upload and the API is getUploadParamsFor*
func PromptAndUploadFileIfNeeded(r *Request, api string, response map[string]interface{}) {
	if !r.Config.HasShell {
		return
	}
	apiName := strings.ToLower(api)
	if apiName != "getuploadparamsforiso" &&
		apiName != "getuploadparamsforvolume" &&
		apiName != "getuploadparamsfotemplate" {
		return
	}
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
	spinner := r.Config.StartSpinner(uploadingMessage)
	errored := 0
	for i, filePath := range validFiles {
		spinner.Suffix = fmt.Sprintf(" uploading %d/%d %s...", i+1, len(validFiles), filepath.Base(filePath))
		if err := uploadFile(postURL, filePath, signature, expires, metadata, spinner); err != nil {
			spinner.Stop()
			fmt.Println("Error uploading", filePath, ":", err)
			errored++
			spinner.Suffix = fmt.Sprintf(" %s", uploadingMessage)
			spinner.Start()
		}
	}
	r.Config.StopSpinner(spinner)
	if errored > 0 {
		fmt.Printf("🙈 %d out of %d files failed to upload.\n", errored, len(validFiles))
	} else {
		fmt.Println("All files uploaded successfully.")
	}
}

type progressReader struct {
	file         *os.File
	total        int64
	read         int64
	updateSuffix func(percent int)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.file.Read(p)
	if n > 0 {
		pr.read += int64(n)
		percent := int(float64(pr.read) / float64(pr.total) * 100)
		pr.updateSuffix(percent)
	}
	return n, err
}

// uploadFile uploads a single file to the given postURL with the required headers.
func uploadFile(postURL, filePath, signature, expires, metadata string, spinner *spinner.Spinner) error {
	originalSuffix := spinner.Suffix
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return err
	}

	pr := &progressReader{
		file:  file,
		total: fileInfo.Size(),
		updateSuffix: func(percent int) {
			spinner.Suffix = fmt.Sprintf(" %s (%d%%)", originalSuffix, percent)
		},
	}
	if _, err := io.Copy(part, pr); err != nil {
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
	spinner.Stop()
	fmt.Println("Upload successful for:", filePath)
	spinner.Suffix = fmt.Sprintf(" %s", uploadingMessage)
	spinner.Start()
	return nil
}
