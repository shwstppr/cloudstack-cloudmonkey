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

package config

import (
	"runtime"
	"time"

	"github.com/briandowns/spinner"
)

var cursor = []string{"\r⣷ 😸", "\r⣯ 😹", "\r⣟ 😺", "\r⡿ 😻", "\r⢿ 😼", "\r⣻ 😽", "\r⣽ 😾", "\r⣾ 😻"}

func init() {
	if runtime.GOOS == "windows" {
		cursor = []string{"|", "/", "-", "\\"}
	}
}

// StartSpinner starts and returns a waiting cursor that the CLI can use
func (c *Config) StartSpinner(suffix string) *spinner.Spinner {
	if !c.HasShell {
		return nil
	}
	waiter := spinner.New(cursor, 200*time.Millisecond)
	waiter.Suffix = " " + suffix
	waiter.Start()
	c.activeSpinners = append(c.activeSpinners, waiter)
	return waiter
}

// StopSpinner stops the provided spinner if it is valid
func (c *Config) StopSpinner(waiter *spinner.Spinner) {
	if waiter != nil {
		waiter.Stop()
		for i, s := range c.activeSpinners {
			if s == waiter {
				c.activeSpinners = append(c.activeSpinners[:i], c.activeSpinners[i+1:]...)
				break
			}
		}
	}
}

// PauseActiveSpinners stops the spinners without removing them from the acive spinners list, allowing resume.
func (c *Config) PauseActiveSpinners() int {
	count := len(c.activeSpinners)
	for _, s := range c.activeSpinners {
		if s != nil && s.Active() {
			s.Stop()
		}
	}
	return count
}

// ResumePausedSpinners restarts the spinners from the active spinners list if they are not already running.
func (c *Config) ResumePausedSpinners() {
	for _, s := range c.activeSpinners {
		if s != nil && !s.Active() {
			s.Start()
		}
	}
}
